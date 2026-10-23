package generator

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func TestIndependentGoalsShareTopicButNotProgress(t *testing.T) {
	goals := []KnowledgeGoal{learningGoal("g1", "Очередь"), learningGoal("g2", "Архив")}
	groups := []goalCards{
		{GoalID: "g1", Content: Fact{Name: "Очередь", Cards: []Card{FlipCard{CardBase{"Что такое очередь?"}, "Документы, ожидающие проверки."}, InputCard{CardBase{"Что такое очередь?"}, "Документы, ожидающие проверки."}}}},
		{GoalID: "g2", Content: Fact{Name: "Архив", Cards: []Card{FlipCard{CardBase{"Что такое архив?"}, "Документы с завершённой проверкой."}, InputCard{CardBase{"Что такое архив?"}, "Документы с завершённой проверкой."}}}},
	}
	r, e := NewGenerator(stageFixture(goals, groups), Options{}).Generate(context.Background(), Document{Text: "Очередь и архив"})
	if e != nil || len(r.Facts) != 2 || len(r.Cards) != 4 {
		t.Fatalf("%+v %v", r, e)
	}
	if r.Facts[0].Topic != r.Facts[1].Topic || r.Facts[0].ID == r.Facts[1].ID || r.Cards[0].FactID != r.Cards[1].FactID || r.Cards[2].FactID != r.Cards[3].FactID {
		t.Fatal("lost independent progress grouping")
	}
}

const testGoal = `{"goalId":"g1","name":"Удаление","topic":"Индекс","pages":[1],"source":"Сначала удалить запись индекса, затем записать блок индекса, затем пометить запись данных."}`
const testContent = `{"name":"Удаление","flip":{"kind":"flip","question":"Как удалить запись?","answer":"Удалить запись индекса; записать блок; пометить данные."},"input":{"kind":"input","question":"Назовите шаги удаления по порядку.","answer":"Удалить запись индекса; записать блок; пометить данные."},"cards":[]}`

func pdfDocument() Document {
	return Document{ID: "doc", PDFs: []providers.PDF{{Name: "lecture.pdf", Data: []byte("%PDF-test"), Pages: 1}}}
}

func TestPDFThreeStagesPublishOnlyReviewed(t *testing.T) {
	calls, published := 0, 0
	p := providerFunc(func(_ context.Context, r providers.Request) (providers.Response, error) {
		calls++
		if len(r.PDFs) != 1 || r.ReasoningEffort == "" || len(r.Schema) == 0 {
			t.Fatalf("missing native source/reasoning/schema: %+v", r)
		}
		var content string
		switch calls {
		case 1:
			content = `{"issues":[],"goals":[` + testGoal + `],"cards":[],"complete":false,"additions":[]}` // server replaces supplied ID
		case 2:
			var input struct {
				Goals []KnowledgeGoal `json:"goals"`
			}
			if err := json.Unmarshal([]byte(r.User), &input); err != nil {
				t.Fatal(err)
			}
			if len(input.Goals) != 1 || input.Goals[0].GoalID != "g1" {
				t.Fatalf("goals: %+v", input)
			}
			content = `{"issues":[],"cards":[{"goalId":"g1","content":` + testContent + `}],"goals":[],"complete":false,"additions":[]}`
		case 3:
			content = `{"issues":[],"complete":true,"goals":[` + testGoal + `],"cards":[{"goalId":"g1","content":` + testContent + `}],"additions":[]}`
		default:
			t.Fatal("unbounded calls")
		}
		if published != 0 {
			t.Fatal("published before review")
		}
		return providers.Response{Content: []byte(content), Usage: providers.Usage{CostUSD: .01, PromptTokens: 20, CachedTokens: new(10)}}, nil
	})
	r, err := NewGenerator(p, Options{OnBatch: func(Batch) { published++ }}).Generate(context.Background(), pdfDocument())
	if err != nil || calls != 3 || published != 1 || len(r.Cards) != 2 || len(r.Stats.Stages) != 3 || r.Stats.CostUSD != .03 {
		t.Fatalf("result %+v, err %v, calls %d published %d", r, err, calls, published)
	}
}

func TestPDFRejectsInvalidStagesWithoutPublishing(t *testing.T) {
	validCards := `{"issues":[],"cards":[{"goalId":"g1","content":` + testContent + `}],"goals":[],"complete":false,"additions":[]}`
	review := `{"issues":[],"complete":true,"goals":[` + testGoal + `],"cards":[{"goalId":"g1","content":` + testContent + `}],"additions":[]}`
	for _, tc := range []struct {
		name      string
		stage     int
		body      string
		truncated bool
	}{
		{"empty goals", 1, `{"issues":[],"goals":[]}`, false},
		{"bad pages", 1, `{"issues":[],"goals":[{"goalId":"","name":"A","topic":"T","pages":[2],"source":"A"}]}`, false},
		{"unknown goal", 2, `{"issues":[],"cards":[{"goalId":"unknown","content":` + testContent + `}]}`, false},
		{"duplicate goal", 2, `{"issues":[],"cards":[{"goalId":"g1","content":` + testContent + `},{"goalId":"g1","content":` + testContent + `}]}`, false},
		{"empty answer", 2, `{"issues":[],"cards":[{"goalId":"g1","content":{"name":"A","flip":{"kind":"flip","question":"Q","answer":" "},"input":{"kind":"input","question":"Q2","answer":"A"},"cards":[]}}]}`, false},
		{"incomplete review", 3, `{"issues":[],"complete":false,"goals":[],"cards":[],"additions":[]}`, false},
		{"truncated review", 3, review, true},
		{"omitted goals", 3, `{"issues":[],"complete":true,"cards":[],"additions":[]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
				calls++
				b := []string{`{"issues":[],"goals":[` + testGoal + `],"cards":[],"complete":false,"additions":[]}`, validCards, review}[calls-1]
				if calls == tc.stage {
					b = tc.body
					if tc.name != "omitted goals" {
						var fields map[string]any
						if e := json.Unmarshal([]byte(b), &fields); e != nil {
							t.Fatal(e)
						}
						for key, value := range map[string]any{"issues": []any{}, "goals": []any{}, "cards": []any{}, "complete": false, "additions": []any{}} {
							if _, ok := fields[key]; !ok {
								fields[key] = value
							}
						}
						encoded, e := json.Marshal(fields)
						if e != nil {
							t.Fatal(e)
						}
						b = string(encoded)
					}
				}
				return providers.Response{Content: []byte(b), Truncated: calls == tc.stage && tc.truncated}, nil
			})
			r, err := NewGenerator(p, Options{OnBatch: func(Batch) { t.Fatal("published invalid response") }}).Generate(context.Background(), pdfDocument())
			if err == nil || len(r.Cards) != 0 || calls != tc.stage {
				t.Fatalf("err=%v cards=%d calls=%d", err, len(r.Cards), calls)
			}
		})
	}
}

func TestPDFStageFailureRetainsMetrics(t *testing.T) {
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Usage: providers.Usage{CostUSD: .02}}, errors.New("failed")
	})
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), pdfDocument())
	if err == nil || r.Stats.CostUSD != .02 || r.Stats.FailedCalls != 1 || len(r.Stats.Stages) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestPDFReviewCorrectsAndAddsGoalsWithServerIDs(t *testing.T) {
	calls := 0
	p := providerFunc(func(_ context.Context, _ providers.Request) (providers.Response, error) {
		calls++
		var b string
		switch calls {
		case 1:
			b = `{"issues":[],"goals":[` + testGoal + `],"cards":[],"complete":false,"additions":[]}`
		case 2:
			b = `{"issues":[],"goals":[],"cards":[{"goalId":"g1","content":` + testContent + `}],"complete":false,"additions":[]}`
		case 3:
			additional := strings.ReplaceAll(testGoal, `"g1"`, `"new:1"`)
			additional = strings.ReplaceAll(additional, `"Удаление"`, `"Поиск"`)
			extra := strings.ReplaceAll(testContent, "Удаление", "Поиск")
			extra = strings.ReplaceAll(extra, "Как удалить запись?", "Как искать запись?")
			extra = strings.ReplaceAll(extra, "Назовите шаги удаления по порядку.", "Назовите шаги поиска по порядку.")
			corrected := strings.ReplaceAll(testContent, "Удалить запись индекса; записать блок; пометить данные.", "Исправленный порядок шагов")
			b = `{"issues":[],"complete":true,"goals":[` + testGoal + `],"cards":[{"goalId":"g1","content":` + corrected + `},{"goalId":"new:1","content":` + extra + `}],"additions":[` + additional + `]}`
		}
		return providers.Response{Content: []byte(b)}, nil
	})
	r, e := NewGenerator(p, Options{}).Generate(context.Background(), pdfDocument())
	if e != nil || len(r.Facts) != 2 || len(r.Cards) != 4 || r.Artifact.Goals[1].GoalID != "g2" || r.Cards[0].Answer != "Исправленный порядок шагов" {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestPDFBudgetSharedAcrossStages(t *testing.T) {
	calls := 0
	p := providerFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
		done, e := providers.ReserveCall(ctx, .03)
		if e != nil {
			return providers.Response{}, e
		}
		calls++
		done(providers.Usage{CostUSD: .02}, true)
		return providers.Response{Content: []byte(`{"issues":[],"goals":[` + testGoal + `],"cards":[],"complete":false,"additions":[]}`), Usage: providers.Usage{CostUSD: .02}}, nil
	})
	r, e := NewGenerator(p, Options{Budget: providers.BudgetLimits{USD: .04}}).Generate(context.Background(), pdfDocument())
	if !errors.Is(e, providers.ErrBudget) || calls != 1 || len(r.Cards) != 0 || r.Budget.CommittedUSD != .02 {
		t.Fatalf("%+v %v calls %d", r, e, calls)
	}
}

func TestTextUsesTheSameThreeStagesAsPDF(t *testing.T) {
	calls := 0
	p := providerFunc(func(_ context.Context, r providers.Request) (providers.Response, error) {
		calls++
		if !strings.Contains(r.Source, "Индекс ускоряет поиск") {
			t.Fatal("complete source missing")
		}
		b := []string{
			`{"issues":[],"goals":[` + testGoal + `],"cards":[],"complete":false,"additions":[]}`,
			`{"issues":[],"goals":[],"cards":[{"goalId":"g1","content":` + testContent + `}],"complete":false,"additions":[]}`,
			`{"issues":[],"complete":true,"goals":[` + testGoal + `],"cards":[{"goalId":"g1","content":` + testContent + `}],"additions":[]}`,
		}[calls-1]
		return providers.Response{Content: []byte(b)}, nil
	})
	r, e := NewGenerator(p, Options{}).Generate(context.Background(), Document{Text: "Индекс ускоряет поиск"})
	if e != nil || calls != 3 || len(r.Cards) != 2 {
		t.Fatalf("%+v %v calls %d", r, e, calls)
	}
}
