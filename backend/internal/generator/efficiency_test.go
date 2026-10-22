package generator

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/material"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestReviewIsolatesBadEvidenceAndMissingVerdicts(t *testing.T) {
	d := *semanticDoc().Structure
	topic := Topic{BlockIDs: []string{"p32-b1"}, Objectives: []Objective{{ID: "a", Text: "Целостность", BlockIDs: []string{"p32-b1"}}}}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return `{"verdicts":[{"index":0,"accept":true,"evidence":[{"blockId":"p32-b1","kind":"text","quote":"Внешний ключ обеспечивает ссылочную целостность."}],"covered":["a"]},{"index":1,"accept":true,"evidence":[{"blockId":"p32-b1","kind":"text","quote":"invented"}],"covered":["a"]}]}`, nil
	}}
	vs, err := NewGenerator(p, Options{}).review(context.Background(), d, topic, []reviewCandidate{{Index: 0}, {Index: 1}, {Index: 2}}, nil)
	if err != nil || len(vs) != 3 || !vs[0].Accept || vs[1].Accept || vs[2].Accept || vs[1].Problem == "" || vs[2].Problem == "" {
		t.Fatalf("healthy card lost with bad siblings: %+v %v", vs, err)
	}
}

func TestTextOnlyTopicDoesNotResendPageImages(t *testing.T) {
	d := material.Document{Pages: []material.Page{{Number: 1, Image: []byte("image")}}, Blocks: []material.Block{{ID: "text", Page: 1, Kind: "text", Text: "Определение термина."}}}
	topic := Topic{BlockIDs: []string{"text"}}
	if len(topicImages(topic, d)) != 0 {
		t.Fatal("text-only task resends image")
	}
	source, _ := topicSource(topic, d, 24000)
	if strings.Contains(source, "[image") {
		t.Fatal("prompt names image that was not sent")
	}
	d.Blocks[0].Kind = "diagram"
	if len(topicImages(topic, d)) != 1 {
		t.Fatal("visual basis lost")
	}
}

func TestReviewKeepsScopeAndDefinitionsButOmitsUnrelatedExamples(t *testing.T) {
	d := material.Document{Blocks: []material.Block{
		{ID: "scope", Page: 2, Kind: "text", Text: "Все следующие действия относятся к SQLite."},
		{ID: "rule", Page: 3, Kind: "text", Text: "NO ACTION — действие по умолчанию."},
		{ID: "example", Page: 4, Kind: "code", Text: strings.Repeat("unrelated example ", 300)},
	}}
	topic := Topic{Title: "Действия", BlockIDs: []string{"scope", "rule", "example"}, Objectives: []Objective{{ID: "default", BlockIDs: []string{"scope", "rule"}}, {ID: "other", BlockIDs: []string{"example"}}}}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if !strings.Contains(r.User, "SQLite") || !strings.Contains(r.User, "NO ACTION") || strings.Contains(r.User, "unrelated example") {
			t.Errorf("wrong review context: %s", r.User)
		}
		return `{"verdicts":[{"index":0,"accept":false,"problem":"scope missing","evidence":[],"covered":[]}]}`, nil
	}}
	_, err := NewGenerator(p, Options{}).review(context.Background(), d, topic, []reviewCandidate{{Index: 0, Evidence: []material.Evidence{{BlockID: "rule", Kind: "text", Quote: "NO ACTION"}}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDocumentRepairsHaveOneSharedLimitAndPrioritizeCoreGaps(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: "Понятие A."}, {ID: "b", Text: "Важное понятие B."}, {ID: "c", Text: "Понятие C."}}}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.Contains(r.System, "STAGE:PLAN") {
			return `{"topics":[{"id":"a","title":"A","blockIds":["a"],"objectives":[{"id":"a-goal","text":"A","priority":"detail","blockIds":["a"]}]},{"id":"b","title":"B","blockIds":["b"],"objectives":[{"id":"b-goal","text":"B","priority":"core","blockIds":["b"]}]},{"id":"c","title":"C","blockIds":["c"],"objectives":[{"id":"c-goal","text":"C","priority":"supporting","blockIds":["c"]}]}]}`, nil
		}
		return `{"facts":[]}`, nil
	}}
	r, err := NewGenerator(p, Options{MaxCoverageRepairs: 1}).Generate(context.Background(), Document{Structure: &d})
	if err != nil || r.Stats.CoverageRetries != 1 || len(r.Coverage.Missing) != 3 {
		t.Fatalf("unbounded or hidden gaps: %+v %v", r, err)
	}
	// plan + all three first attempts + one repair; lower-priority gaps remain.
	if len(p.requests) != 5 {
		t.Fatalf("calls=%d want 5", len(p.requests))
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if !strings.Contains(p.requests[len(p.requests)-1].User, "b-goal") {
		t.Fatal("repair spent on detail before core")
	}
}

func TestReviewMalformedSiblingDoesNotForcePaidRetry(t *testing.T) {
	d := *semanticDoc().Structure
	topic := Topic{BlockIDs: []string{"p32-b1"}}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return `{"verdicts":[{"index":0,"accept":true,"evidence":[{"blockId":"p32-b1","kind":"text","quote":"Внешний ключ обеспечивает ссылочную целостность."}],"covered":[]},{"index":1,"accept":true,"evidence":"broken","covered":[]}]}`, nil
	}}
	vs, err := NewGenerator(p, Options{}).review(context.Background(), d, topic, []reviewCandidate{{Index: 0}, {Index: 1}}, nil)
	if err != nil || len(vs) != 2 || !vs[0].Accept || vs[1].Accept || len(p.requests) != 1 {
		t.Fatalf("bad sibling cost healthy card/retry: %v %+v calls=%d", err, vs, len(p.requests))
	}
}

func TestObjectiveScopeCanBeGroundedInDistantIntroductoryBlock(t *testing.T) {
	d := *semanticDoc().Structure
	p := Plan{Topics: []Topic{{ID: "fk", Title: "Ключи", BlockIDs: []string{"p32-b1", "p33-b1"}, Objectives: []Objective{{ID: "a", Text: "Область правила", Scope: []string{"SQLite"}, BlockIDs: []string{"p32-b1"}}}}}}
	bindObjectiveScopes(&p, d)
	if err := validatePlan(p, d); err != nil {
		t.Fatal(err)
	}
	if len(p.Topics[0].Objectives[0].BlockIDs) != 2 {
		t.Fatal("scope did not retain its actual source")
	}
	p.Topics[0].Objectives[0].Scope = []string{"external system"}
	bindObjectiveScopes(&p, d)
	if validatePlan(p, d) == nil {
		t.Fatal("invented scope accepted")
	}
}

func TestSemanticExplanatoryChoiceUsesOpenAnswerBeforeReview(t *testing.T) {
	quote := "В SQLite действие RESTRICT запускается при изменении поля записи, а не по завершении текущего запроса."
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: quote}}}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return `{"facts":[{"quote":"` + quote + `","topic":"Ограничения целостности","name":"Момент проверки ограничения","evidence":[],"cards":[{"kind":"choice","learningMode":"distinguish","question":"В SQLite чем действие RESTRICT отличается по моменту срабатывания?","answer":"Оно срабатывает при изменении поля записи, а не по завершении текущего запроса","distractors":["При закрытии базы","При открытии базы","При чтении строки"],"explanation":"Условие проверяется при изменении поля записи."}]}]}`, nil
	}}
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), Document{Text: quote, EvidenceSource: &d})
	if err != nil || len(r.Cards) != 1 || r.Cards[0].Kind != "flip" || r.Stats.ExplanatoryChoiceToFlip != 1 {
		t.Fatalf("valid paraphrase lost before semantic review: %v %+v", err, r)
	}
}

func TestPlanningHasIndependentBudgetAndPreservesWholeTopicContext(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "a", Text: strings.Repeat("A", 7000)}, {ID: "b", Text: strings.Repeat("B", 7000)}}}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.Contains(r.System, "STAGE:PLAN-MERGE-CHECK") {
			return `{"groups":[{"topicIds":["section1-t","section2-t"],"reason":"Same learning topic"}]}`, nil
		}
		if strings.Contains(r.System, "STAGE:PLAN-MERGE") {
			return `{"groups":[["section1-t","section2-t"]]}`, nil
		}
		var bs []material.Block
		if err := json.Unmarshal([]byte(r.User), &bs); err != nil {
			t.Fatal(err)
		}
		if len(bs) != 1 || len(bs[0].Text) != 7000 {
			t.Fatal("planning split truncated source or ignored budget")
		}
		return `{"topics":[{"id":"t","title":"Topic","blockIds":["` + bs[0].ID + `"],"objectives":[{"id":"o","text":"Goal","blockIds":["` + bs[0].ID + `"]}]}]}`, nil
	}}
	plan, err := NewGenerator(p, Options{SourceBudget: 20000, PlanningBudget: 8000}).plan(context.Background(), d, nil)
	if err != nil || len(plan.Topics) != 1 || len(p.requests) != 4 {
		t.Fatalf("planning: %+v %v calls=%d", plan, err, len(p.requests))
	}
	source, err := topicSource(plan.Topics[0], d, 20000)
	if err != nil || !strings.Contains(source, d.Blocks[0].Text) || !strings.Contains(source, d.Blocks[1].Text) {
		t.Fatal("topic context shrank with planning budget")
	}
}

func TestDuplicateMergeKeepsAllSourceGoalsWithoutAnotherPaidAttempt(t *testing.T) {
	parts := []Plan{{Topics: []Topic{{ID: "a", Title: "A", BlockIDs: []string{"a"}, Objectives: []Objective{{ID: "oa", Text: "A", BlockIDs: []string{"a"}}}}}}, {Topics: []Topic{{ID: "b", Title: "B", BlockIDs: []string{"b"}, Objectives: []Objective{{ID: "ob", Text: "B", BlockIDs: []string{"b"}}}}}}}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) { return `{"groups":[["a","b"],["a"]]}`, nil }}
	plan, err := NewGenerator(p, Options{}).mergePlans(context.Background(), parts, nil)
	if err != nil || len(plan.Topics) != 2 || len(plan.Issues) == 0 || len(p.requests) != 1 {
		t.Fatalf("recoverable merge killed work: %+v %v", plan, err)
	}
}

func TestOversizedReviewCardDoesNotDiscardHealthySibling(t *testing.T) {
	d := *semanticDoc().Structure
	topic := Topic{BlockIDs: []string{"p32-b1"}}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		var input struct {
			Cards []reviewCandidate `json:"cards"`
		}
		if err := json.Unmarshal([]byte(r.User), &input); err != nil {
			t.Fatal(err)
		}
		if len(input.Cards) != 1 || input.Cards[0].Question != "Healthy?" {
			t.Fatal("oversized card reached model")
		}
		return `{"verdicts":[{"index":0,"accept":true,"evidence":[{"blockId":"p32-b1","kind":"text","quote":"Внешний ключ обеспечивает ссылочную целостность."}],"covered":[]}]}`, nil
	}}
	vs, err := NewGenerator(p, Options{SourceBudget: 1000}).review(context.Background(), d, topic, []reviewCandidate{{Index: 0, Question: strings.Repeat("large ", 1000)}, {Index: 1, Question: "Healthy?"}}, nil)
	if err != nil || len(vs) != 2 || vs[0].Accept || !vs[1].Accept || vs[1].Index != 1 || len(p.requests) != 1 {
		t.Fatalf("bad context killed healthy card: %+v %v", vs, err)
	}
}

func TestSourceScopeIsCheckedEvenWhenReviewerOmitsCoverage(t *testing.T) {
	quote := "В SQLite действие по умолчанию — NO ACTION."
	d := material.Document{Blocks: []material.Block{{ID: "rule", Text: quote}}}
	topic := Topic{ID: "t", Title: "Действия", BlockIDs: []string{"rule"}, Objectives: []Objective{{ID: "o", Text: "Действие по умолчанию", BlockIDs: []string{"rule"}, Scope: []string{"SQLite"}}}}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		if strings.HasPrefix(r.System, "STAGE:REVIEW") {
			return `{"verdicts":[{"index":0,"accept":true,"problem":"","evidence":[{"blockId":"rule","kind":"text","quote":"` + quote + `"}],"covered":[]}]}`, nil
		}
		return answerJSON(mc{kind: "flip", q: "Какое действие внешнего ключа используется по умолчанию?", a: "NO ACTION", quote: quote}), nil
	}}
	r := NewGenerator(p, Options{}).runTopic(context.Background(), Document{ID: "doc", Structure: &d}, topic, nil)
	if r.err != nil || len(r.Cards) != 0 || len(r.Reviews) != 1 || !strings.Contains(r.Reviews[0].Problem, "SQLite") {
		t.Fatalf("reviewer's missing coverage bypassed scope gate: %+v", r)
	}
}

func TestTableCellCanBePrimarySourceWithoutFlattenedText(t *testing.T) {
	d := material.Document{Blocks: []material.Block{{ID: "types", Kind: "table", Cells: [][]string{{"Тип", "Значение"}, {"integer", "Целые числа"}}}}}
	evidence, quote, ok := groundFact(modelFact{Quote: "Целые числа"}, d)
	if !ok || quote != "Целые числа" || len(evidence) != 1 || evidence[0].Kind != "cell" || evidence[0].Row != 1 || evidence[0].Column != 1 {
		t.Fatalf("table-only source lost: %+v %q %v", evidence, quote, ok)
	}
	if _, _, ok = groundFact(modelFact{Quote: "integer Целые числа"}, d); ok {
		t.Fatal("different cells joined into fabricated quotation")
	}
}

func TestReviewCannotAcceptCardWhileReportingItsDefect(t *testing.T) {
	d := *semanticDoc().Structure
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		return `{"verdicts":[{"index":0,"accept":true,"problem":"Lost source condition","evidence":[{"blockId":"p32-b1","kind":"text","quote":"Внешний ключ обеспечивает ссылочную целостность."}],"covered":[]}]}`, nil
	}}
	vs, err := NewGenerator(p, Options{}).review(context.Background(), d, Topic{BlockIDs: []string{"p32-b1"}}, []reviewCandidate{{Index: 0}}, nil)
	if err != nil || len(vs) != 1 || vs[0].Accept || vs[0].Problem != "Lost source condition" {
		t.Fatalf("contradictory approval accepted: %+v %v", vs, err)
	}
}

type reservingProvider struct{ providers.Provider }

func (p reservingProvider) Complete(ctx context.Context, r providers.Request) (providers.Response, error) {
	finish, err := providers.ReserveCall(ctx, .01)
	if err != nil {
		return providers.Response{}, err
	}
	result, err := p.Provider.Complete(ctx, r)
	finish(result.Usage, err == nil)
	return result, err
}

func TestBudgetExhaustionRetainsStreamedCardsAndHonestGaps(t *testing.T) {
	d := semanticDoc()
	base := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		switch {
		case strings.HasPrefix(r.System, "STAGE:PLAN"):
			return `{"topics":[{"id":"a","title":"Назначение ключа","blockIds":["p22-b1","p32-b1"],"objectives":[{"id":"integrity","text":"Назначение","blockIds":["p32-b1"]}]},{"id":"b","title":"Условия","blockIds":["p33-b1"],"objectives":[{"id":"scope","text":"Область","blockIds":["p33-b1"]}]}]}`, nil
		case strings.HasPrefix(r.System, "STAGE:REVIEW"):
			return `{"verdicts":[{"index":0,"accept":true,"problem":"","evidence":[{"blockId":"p32-b1","kind":"text","quote":"Внешний ключ обеспечивает ссылочную целостность."}],"covered":["integrity"]}]}`, nil
		default:
			return answerJSON(mc{kind: "flip", q: "Для чего нужен внешний ключ?", a: "Обеспечивает ссылочную целостность.", quote: "Внешний ключ обеспечивает ссылочную целостность."}), nil
		}
	}}
	ledger := providers.NewBudget(providers.BudgetLimits{Calls: 5})
	ctx := providers.WithBudget(context.Background(), ledger)
	finish, err := providers.ReserveCall(ctx, .01)
	if err != nil {
		t.Fatal(err)
	}
	finish(providers.Usage{}, true) // reading used the same document ledger
	streamed := 0
	result, err := NewGenerator(reservingProvider{base}, Options{OnBatch: func(b Batch) { streamed += len(b.Cards) }}).Generate(ctx, d)
	if err != nil || streamed != 1 || len(result.Cards) != 1 || len(result.Coverage.Missing) != 1 || result.Coverage.Missing[0] != "scope" || ledger.Snapshot().Calls != 5 || len(base.requests) != 4 {
		t.Fatalf("budget discarded progress or started extra calls: %+v err=%v calls=%d", result, err, len(base.requests))
	}
	if !slices.ContainsFunc(result.Coverage.Issues, func(s string) bool { return strings.Contains(s, providers.ErrBudget.Error()) }) {
		t.Fatal("budget stop concealed from report")
	}
}

func TestVisualEvidenceForcesOriginalImageIntoReview(t *testing.T) {
	region := material.Region{0, 0, 1, 1}
	d := material.Document{Pages: []material.Page{{Number: 1, Image: []byte("original"), ImageMime: "image/jpeg"}}, Blocks: []material.Block{{ID: "table", Page: 1, Kind: "table", Text: "A B", Cells: [][]string{{"A", "B"}}}}}
	e := material.Evidence{BlockID: "table", Kind: "image", Region: &region, Observation: "Стрелка от A к B"}
	p := &fakeProvider{answer: func(_ int, _ string, r providers.Request) (string, error) {
		images := providers.RequestImages(r)
		if len(images) != 1 || string(images[0].Data) != "original" {
			t.Error("visual verification did not receive original image")
		}
		raw, _ := json.Marshal(map[string]any{"verdicts": []verdict{{Index: 0, Accept: true, Evidence: []material.Evidence{e}}}})
		return string(raw), nil
	}}
	_, err := NewGenerator(p, Options{}).review(context.Background(), d, Topic{BlockIDs: []string{"table"}}, []reviewCandidate{{Index: 0, Evidence: []material.Evidence{e}}}, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestReviewerCannotClaimToInspectImageItDidNotReceive(t *testing.T) {
	region := material.Region{0, 0, 1, 1}
	d := material.Document{Pages: []material.Page{{Number: 1, Image: []byte("original")}}, Blocks: []material.Block{{ID: "text", Page: 1, Kind: "text", Text: "Source text."}}}
	p := &fakeProvider{answer: func(int, string, providers.Request) (string, error) {
		raw, _ := json.Marshal(map[string]any{"verdicts": []verdict{{Index: 0, Accept: true, Evidence: []material.Evidence{{BlockID: "text", Kind: "image", Region: &region, Observation: "Invented visual inspection"}}}}})
		return string(raw), nil
	}}
	vs, err := NewGenerator(p, Options{}).review(context.Background(), d, Topic{BlockIDs: []string{"text"}}, []reviewCandidate{{Index: 0}}, nil)
	if err != nil || len(vs) != 1 || vs[0].Accept {
		t.Fatalf("unseen image accepted: %+v %v", vs, err)
	}
}
