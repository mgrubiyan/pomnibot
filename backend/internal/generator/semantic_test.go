package generator

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func semanticDoc() Document {
	d := material.Document{Version: material.Version, Blocks: []material.Block{
		{ID: "p22-b1", Page: 22, Kind: "code", Text: "FOREIGN KEY (pid) REFERENCES Parent(id)"},
		{ID: "p32-b1", Page: 32, Text: "Внешний ключ обеспечивает ссылочную целостность."},
		{ID: "p33-b1", Page: 33, Text: "В SQLite проверка внешних ключей включается отдельно."},
	}}
	return Document{Title: "SQL", Structure: &d}
}

func TestSemanticPlanRequiresRealSourceAndPreservesDistantContext(t *testing.T) {
	d := semanticDoc()
	p := Plan{Topics: []Topic{{ID: "fk", Title: "Внешние ключи", BlockIDs: []string{"p22-b1", "p32-b1", "p33-b1"}, Objectives: []Objective{{ID: "scope", Text: "Различать правило и условия SQLite", BlockIDs: []string{"p33-b1"}}}}}}
	if err := validatePlan(p, *d.Structure); err != nil {
		t.Fatal(err)
	}
	text, err := topicSource(p.Topics[0], *d.Structure, 24000)
	if err != nil || !strings.Contains(text, "Parent(id)") || !strings.Contains(text, "SQLite") {
		t.Fatalf("context lost: %s %v", text, err)
	}
	p.Topics[0].BlockIDs = append(p.Topics[0].BlockIDs, "invented")
	if validatePlan(p, *d.Structure) == nil {
		t.Fatal("accepted invented source")
	}
}

func TestSemanticPipelineOnlyStreamsReviewedCardsAndTargetsMissingCoverage(t *testing.T) {
	d := semanticDoc()
	plan := `{"topics":[{"id":"fk","title":"Внешние ключи","section":"SQL","blockIds":["p22-b1","p32-b1","p33-b1"],"objectives":[{"id":"integrity","text":"Назначение внешнего ключа","blockIds":["p32-b1"]},{"id":"scope","text":"Условия SQLite","blockIds":["p33-b1"]}]}]}`
	generations, delivered := 0, 0
	var reviewCount atomic.Int32
	model := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		switch {
		case strings.Contains(r.System, "STAGE:PLAN"):
			return plan, nil
		case strings.Contains(r.System, "STAGE:REVIEW"):
			reviewCount.Add(1)
			covered := "integrity"
			block, quote := "p32-b1", "Внешний ключ обеспечивает ссылочную целостность."
			if strings.Contains(r.User, "Как включается проверка") {
				covered, block, quote = "scope", "p33-b1", "В SQLite проверка внешних ключей включается отдельно."
			}
			b, _ := json.Marshal(map[string]any{"verdicts": []any{map[string]any{"index": 0, "accept": true, "problem": "", "evidence": []material.Evidence{{BlockID: block, Quote: quote}}, "covered": []string{covered}}}})
			return string(b), nil
		default:
			generations++
			if !strings.Contains(r.User, "Parent(id)") || !strings.Contains(r.User, "SQLite") {
				t.Error("generation missing distant context")
			}
			if generations == 1 {
				return answerJSON(mc{kind: "flip", q: "Для чего служит внешний ключ?", a: "Обеспечивает ссылочную целостность.", quote: "Внешний ключ обеспечивает ссылочную целостность."}), nil
			}
			if !strings.Contains(r.User, "scope") {
				t.Error("coverage retry is not targeted")
			}
			return answerJSON(mc{kind: "flip", q: "Как включается проверка внешних ключей в SQLite?", a: "Отдельно.", quote: "В SQLite проверка внешних ключей включается отдельно."}), nil
		}
	}}
	gen := NewGenerator(model, Options{OnBatch: func(b Batch) {
		if reviewCount.Load() == 0 {
			t.Error("unreviewed batch escaped")
		}
		delivered += len(b.Cards)
	}})
	res, err := gen.Generate(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Cards) != 2 || delivered != 2 || generations != 2 || len(res.Coverage.Missing) != 0 {
		t.Fatalf("cards=%d delivered=%d generations=%d coverage=%+v", len(res.Cards), delivered, generations, res.Coverage)
	}
	if res.Cards[0].Evidence[0].BlockID != "p32-b1" {
		t.Fatal("lost provenance")
	}
}

func TestReviewRejectsMissingVerdictsAndUnsupportedAcceptance(t *testing.T) {
	d := semanticDoc()
	cs := []reviewCandidate{{Index: 0, Question: "Верно ли?", Answer: "Да"}}
	for _, response := range []string{`{"verdicts":[]}`, `{"verdicts":[{"index":0,"accept":true,"problem":"","evidence":[{"blockId":"p33-b1","quote":"Любой SQL"}],"covered":[]}]}`} {
		model := &fakeProvider{answer: func(int, string, providers.Request) (string, error) { return response, nil }}
		gen := NewGenerator(model, Options{})
		v, err := gen.review(context.Background(), *d.Structure, Topic{}, cs, nil)
		if err == nil && len(v) > 0 && v[0].Accept {
			t.Fatal("unsupported review accepted")
		}
	}
}

func TestHierarchicalMergeCannotLoseGoalsOrDistantSources(t *testing.T) {
	a := Plan{Topics: []Topic{{ID: "early", Title: "Внешние ключи", BlockIDs: []string{"p22-b1"}, Objectives: []Objective{{ID: "declaration", Text: "Объявление", BlockIDs: []string{"p22-b1"}}}}}}
	b := Plan{Topics: []Topic{{ID: "late", Title: "Действия внешнего ключа", BlockIDs: []string{"p32-b1", "p33-b1"}, Objectives: []Objective{{ID: "actions", Text: "Действия", BlockIDs: []string{"p32-b1", "p33-b1"}}}}}}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.Contains(r.System, "STAGE:PLAN-MERGE-CHECK") {
			return `{"groups":[{"topicIds":["early","late"],"reason":"One topic and its rules"}]}`, nil
		}
		return `{"groups":[["early","late"]]}`, nil
	}}
	merged, err := NewGenerator(p, Options{}).mergePlans(context.Background(), []Plan{a, b}, nil)
	if err != nil || len(merged.Topics) != 1 || len(merged.Topics[0].Objectives) != 2 || len(merged.Topics[0].BlockIDs) != 3 {
		t.Fatalf("lost goals/sources: %v %+v", err, merged)
	}
	p.answer = func(int, string, providers.Request) (string, error) { return `{"groups":[["early"]]}`, nil }
	fallback, err := NewGenerator(p, Options{}).mergePlans(context.Background(), []Plan{a, b}, nil)
	if err != nil || len(fallback.Topics) != 2 || len(fallback.Issues) == 0 {
		t.Fatal("omitted topic was not restored with an explicit issue", err)
	}
}

func TestEvidenceFormattingResolvesToOriginalContinuousSpan(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: "В SQLite\nдействие — NO ACTION."}, {ID: "b", Text: "Другой фрагмент."}}}
	e, ok := resolveEvidence(d, material.Evidence{BlockID: "a", Kind: "text", Quote: "В SQLite действие — NO ACTION."})
	if !ok || e.Quote != "В SQLite\nдействие — NO ACTION." {
		t.Fatal("formatting variation not resolved to exact source")
	}
	if _, ok = resolveEvidence(d, material.Evidence{BlockID: "a", Quote: "NO ACTION. Другой фрагмент."}); ok {
		t.Fatal("fabricated joined evidence accepted")
	}
}

func TestApplicationCanUseValuesGivenInSelfContainedCode(t *testing.T) {
	source := "UPDATE T SET value = 500 WHERE id = 2;"
	d := material.Document{Blocks: []material.Block{{ID: "code", Page: 28, Kind: "code", Text: source}}}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		data := map[string]any{"facts": []any{map[string]any{"quote": source, "topic": "Изменение записей", "name": "Условие обновления", "cards": []any{map[string]any{"kind": "flip", "learningMode": "apply", "question": "Какая строка изменится?\n```sql\n" + source + "\n```", "answer": "Строка с id = 2.", "explanation": "Условие WHERE выбирает строку с id = 2.", "distractors": []string{}}}}}}
		encoded, _ := json.Marshal(data)
		return string(encoded), nil
	}}
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Text: source, EvidenceSource: &d})
	if err != nil || len(r.Cards) != 1 {
		t.Fatalf("application rejected because input data appeared in question: %v %+v", err, r.Stats)
	}
}

func TestRepeatedQuotationMustBeGroundedInsideCurrentTask(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "earlier", Page: 14, Text: "CREATE INDEX – создать индекс"}, {ID: "current", Page: 23, Text: "CREATE INDEX – создать индекс"}}}
	task := Topic{ID: "index", Title: "Индексы", BlockIDs: []string{"current"}, Objectives: []Objective{{ID: "create", Text: "Создание индекса", BlockIDs: []string{"current"}}}}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.Contains(r.System, "STAGE:REVIEW") {
			if strings.Contains(r.User, `"blockId":"earlier"`) {
				t.Error("grounding escaped the current task")
			}
			return `{"verdicts":[{"index":0,"accept":true,"problem":"","evidence":[{"blockId":"current","kind":"text","quote":"CREATE INDEX – создать индекс","row":0,"column":0}],"covered":["create"]}]}`, nil
		}
		return answerJSON(mc{kind: "flip", q: "Какую команду используют для создания индекса?", a: "CREATE INDEX", quote: "CREATE INDEX – создать индекс"}), nil
	}}
	r := NewGenerator(p, Options{}).runTopic(context.Background(), Document{ID: "doc", Structure: &d}, task, nil)
	if r.err != nil || len(r.Cards) != 1 || r.Cards[0].Evidence[0].BlockID != "current" {
		t.Fatalf("task grounding failed: %v cards=%v", r.err, r.Cards)
	}
}
