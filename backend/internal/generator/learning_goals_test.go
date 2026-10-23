package generator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

const testGoal = `{"goalId":"local-a","name":"Удаление","topic":"Индекс","pages":[1],"source":"Сначала удалить запись индекса, затем записать блок индекса, затем пометить запись данных."}`
const testContent = `{"name":"Удаление","question":"Как удалить запись?","answer":"Удалить запись индекса; записать блок; пометить данные.","cards":[]}`

func pdfDocument() Document {
	return Document{ID: "doc", PDFs: []providers.PDF{{Name: "lecture.pdf", Data: []byte("%PDF-test"), Pages: 1}}}
}
func directReply() string {
	return `{"issues":[],"goals":[` + testGoal + `],"cards":[{"goalId":"local-a","content":` + testContent + `}]}`
}
func TestDirectGenerationPublishesOnceAfterValidation(t *testing.T) {
	for _, doc := range []Document{pdfDocument(), {Text: "Индекс ускоряет поиск"}} {
		calls, published := 0, 0
		p := providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
			calls++
			if published != 0 || len(req.Schema) == 0 || req.ReasoningEffort == "" || !req.NoFallback {
				t.Fatal("missing request controls or early publication")
			}
			if len(doc.PDFs) > 0 && len(req.PDFs) != 1 {
				t.Fatal("missing original PDF")
			}
			if doc.Text != "" && !strings.Contains(req.Source, doc.Text) {
				t.Fatal("missing original text")
			}
			return providers.Response{Content: []byte(directReply()), Usage: providers.Usage{CostUSD: .01, PromptTokens: 20, CompletionTokens: 10}}, nil
		})
		r, err := NewGenerator(p, Options{OnBatch: func(b Batch) {
			published++
			if len(b.Cards) != 2 || len(b.Facts) != 1 {
				t.Fatal("partial batch")
			}
		}}).Generate(context.Background(), doc)
		if err != nil || calls != 1 || published != 1 || len(r.Stats.Stages) != 1 || r.Stats.CostUSD != .01 || r.Stats.PromptTokens != 20 || r.Stats.CompletionTokens != 10 {
			t.Fatalf("%+v %v calls=%d published=%d", r, err, calls, published)
		}
		if r.Artifact.Goals[0].GoalID != "g1" || r.Cards[0].FactID != r.Facts[0].ID || r.Cards[1].FactID != r.Facts[0].ID {
			t.Fatal("lost server IDs or relationships")
		}
	}
}
func TestDirectGenerationRejectsInvalidResponse(t *testing.T) {
	for _, defect := range []string{"truncated flag", "broken JSON", "missing goals", "missing issues", "unknown goal", "duplicate goal", "missing group", "bad page", "blank answer", "missing question", "missing answer", "duplicate group", "mismatched name"} {
		t.Run(defect, func(t *testing.T) {
			b := directReply()
			switch defect {
			case "broken JSON":
				b = b[:len(b)-5]
			case "missing goals":
				b = strings.Replace(b, `"goals":`, `"other":`, 1)
			case "missing issues":
				b = strings.Replace(b, `"issues":[],`, "", 1)
			case "unknown goal":
				b = strings.Replace(b, `"cards":[{"goalId":"local-a"`, `"cards":[{"goalId":"unknown"`, 1)
			case "duplicate goal":
				b = strings.Replace(b, `"goals":[`+testGoal+`]`, `"goals":[`+testGoal+`,`+testGoal+`]`, 1)
			case "missing group":
				b = `{"issues":[],"goals":[` + testGoal + `],"cards":[]}`
			case "bad page":
				b = strings.Replace(b, `"pages":[1]`, `"pages":[2]`, 1)
			case "blank answer":
				b = strings.ReplaceAll(b, `"answer":"Удалить запись индекса; записать блок; пометить данные."`, `"answer":" "`)
			case "missing question":
				b = strings.Replace(b, `"question":`, `"other":`, 1)
			case "missing answer":
				b = strings.Replace(b, `"answer":`, `"other":`, 1)
			case "duplicate group":
				b = strings.Replace(b, `"cards":[{"goalId":"local-a","content":`+testContent+`}]`, `"cards":[{"goalId":"local-a","content":`+testContent+`},{"goalId":"local-a","content":`+testContent+`}]`, 1)
			case "mismatched name":
				b = strings.Replace(b, `"content":{"name":"Удаление"`, `"content":{"name":"Другое"`, 1)
			}
			calls := 0
			p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
				calls++
				return providers.Response{Content: []byte(b), Truncated: defect == "truncated flag"}, nil
			})
			r, err := NewGenerator(p, Options{OnBatch: func(Batch) { t.Fatal("published invalid response") }}).Generate(context.Background(), pdfDocument())
			if err == nil || calls != 1 || len(r.Cards) != 0 || len(r.Facts) != 0 || r.Stats.InvalidResponses != 1 {
				t.Fatalf("%+v %v calls=%d", r, err, calls)
			}
		})
	}
}
func TestDirectGenerationKeepsIssuesAndFailureMetrics(t *testing.T) {
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Content: []byte(strings.Replace(directReply(), `"issues":[]`, `"issues":["Page 1 contains an unreadable fragment"]`, 1))}, nil
	})
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), pdfDocument())
	if err != nil || len(r.Cards) != 2 || len(r.Coverage.Issues) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	p = providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Usage: providers.Usage{CostUSD: .02}}, errors.New("failed")
	})
	r, err = NewGenerator(p, Options{}).Generate(context.Background(), pdfDocument())
	if err == nil || r.Stats.CostUSD != .02 || r.Stats.FailedCalls != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}
func TestDirectGenerationSharesExistingBudget(t *testing.T) {
	for _, remaining := range []bool{true, false} {
		ledger := providers.NewBudget(providers.BudgetLimits{USD: .05})
		done, err := providers.ReserveCall(providers.WithBudget(context.Background(), ledger), .03)
		if err != nil {
			t.Fatal(err)
		}
		done(providers.Usage{CostUSD: .03}, true) // prior reading or failed attempt
		calls := 0
		p := providerFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
			bound := .02
			if !remaining {
				bound = .03
			}
			finish, e := providers.ReserveCall(ctx, bound)
			if e != nil {
				return providers.Response{}, e
			}
			calls++
			finish(providers.Usage{CostUSD: .01}, true)
			return providers.Response{Content: []byte(directReply()), Usage: providers.Usage{CostUSD: .01, Attempts: 1}}, nil
		})
		r, e := NewGenerator(p, Options{}).Generate(providers.WithBudget(context.Background(), ledger), pdfDocument())
		if remaining {
			if e != nil || calls != 1 || r.Budget.CommittedUSD != .04 {
				t.Fatalf("%+v %v", r, e)
			}
		} else if !errors.Is(e, providers.ErrBudget) || calls != 0 || len(r.Cards) != 0 || r.Budget.CommittedUSD != .03 {
			t.Fatalf("%+v %v", r, e)
		}
	}
}
func TestDirectGenerationRejectsFactLimitWithoutPartialPublication(t *testing.T) {
	goals := []KnowledgeGoal{learningGoal("a", "A"), learningGoal("b", "B")}
	groups := []goalCards{}
	for _, g := range goals {
		f := Fact{Name: g.Name, Cards: []Card{FlipCard{CardBase{g.Name + "?"}, "answer"}, InputCard{CardBase{g.Name + "?"}, "answer"}}}
		groups = append(groups, goalCards{GoalID: g.GoalID, Content: f})
	}
	b, err := json.Marshal(map[string]any{"issues": []string{}, "goals": goals, "cards": groups})
	if err != nil {
		t.Fatal(err)
	}
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Content: b}, nil
	})
	r, e := NewGenerator(p, Options{MaxFactsPerDoc: 1, OnBatch: func(Batch) { t.Fatal("published over limit") }}).Generate(context.Background(), pdfDocument())
	if e == nil || len(r.Cards) != 0 {
		t.Fatalf("%+v %v", r, e)
	}
}
