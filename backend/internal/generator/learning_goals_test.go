package generator

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/prompts"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

const testFact = `{"name":"Удаление","pages":[1],"recall":{"question":"Как удалить запись?","answer":"Удалить запись индекса; записать блок; пометить данные.","source":"Удаление индекса","sourceQuote":""},"extraCards":[]}`

func pdfDocument() Document {
	return Document{PDFs: []providers.PDF{{Name: "lecture.pdf", Data: []byte("%PDF-test"), Pages: 1}}}
}
func directReply() string { return `{"issues":[],"facts":[` + testFact + `]}` }
func TestDirectGenerationPublishesOnceAfterValidation(t *testing.T) {
	for _, doc := range []Document{pdfDocument(), {Text: "Индекс ускоряет поиск"}} {
		expectedCalls := 2
		if len(doc.PDFs) > 0 {
			expectedCalls++
		}
		calls, published := 0, 0
		p := providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
			calls++
			if published != 0 || len(req.Schema) == 0 || req.ReasoningEffort == "" || !req.NoFallback {
				t.Fatal("missing request controls or early publication")
			}
			if req.System == imageDiscoveryPrompt {
				return providers.Response{Content: []byte(`{"figures":[]}`), Usage: providers.Usage{CostUSD: .01, PromptTokens: 20, CompletionTokens: 10}}, nil
			}
			if req.System == prompts.GeneratorIllustrations {
				return providers.Response{Content: []byte(`{"illustrations":[]}`), Usage: providers.Usage{CostUSD: .01, PromptTokens: 20, CompletionTokens: 10}}, nil
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
			if CardCount(b.Facts) != 2 || len(b.Facts) != 1 {
				t.Fatal("partial batch")
			}
		}}).Generate(context.Background(), doc)
		if err != nil || calls != expectedCalls || published != 1 || len(r.Stats.Stages) != expectedCalls || r.Stats.CostUSD != float64(expectedCalls)*.01 || r.Stats.PromptTokens != expectedCalls*20 || r.Stats.CompletionTokens != expectedCalls*10 {
			t.Fatalf("%+v %v calls=%d published=%d", r, err, calls, published)
		}
		if r.Artifact.Facts[0].Name != r.Facts[0].Name || CardCount(r.Facts) != 2 || r.Artifact.Request == nil || r.Artifact.Response != directReply() {
			t.Fatal("lost nested facts or private request/response")
		}
	}
}
func TestDirectGenerationRejectsInvalidResponse(t *testing.T) {
	for defect, mutate := range map[string]func(string) string{
		"truncated flag": func(s string) string { return s },
		"broken JSON":    func(s string) string { return s[:len(s)-5] },
		"missing facts":  func(s string) string { return strings.Replace(s, `"facts":`, `"other":`, 1) },
		"empty facts":    func(string) string { return `{"issues":[],"facts":[]}` },
		"missing issues": func(s string) string { return strings.Replace(s, `"issues":[],`, "", 1) },
		"null issues":    func(s string) string { return strings.Replace(s, `"issues":[]`, `"issues":null`, 1) },
		"duplicate fact": func(string) string { return `{"issues":[],"facts":[` + testFact + `,` + testFact + `]}` },
		"missing name":   func(s string) string { return strings.Replace(s, `"name":"Удаление",`, "", 1) },
		"missing pages":  func(s string) string { return strings.Replace(s, `"pages":[1],`, "", 1) },
		"missing recall": func(s string) string { return strings.Replace(s, `"recall":`, `"other":`, 1) },
		"blank question": func(s string) string {
			return strings.Replace(s, `"question":"Как удалить запись?"`, `"question":" "`, 1)
		},
		"blank name":      func(s string) string { return strings.Replace(s, `"name":"Удаление"`, `"name":" "`, 1) },
		"bad page":        func(s string) string { return strings.Replace(s, `"pages":[1]`, `"pages":[2]`, 1) },
		"zero page":       func(s string) string { return strings.Replace(s, `"pages":[1]`, `"pages":[0]`, 1) },
		"duplicate pages": func(s string) string { return strings.Replace(s, `"pages":[1]`, `"pages":[1,1]`, 1) },
		"empty pages":     func(s string) string { return strings.Replace(s, `"pages":[1]`, `"pages":[]`, 1) },
		"blank answer": func(s string) string {
			return strings.Replace(s, `"answer":"Удалить запись индекса; записать блок; пометить данные."`, `"answer":" "`, 1)
		},
		"missing question": func(s string) string { return strings.Replace(s, `"question":`, `"other":`, 1) },
		"missing answer":   func(s string) string { return strings.Replace(s, `"answer":`, `"other":`, 1) },
		"missing source":   func(s string) string { return strings.Replace(s, `"source":"Удаление индекса",`, "", 1) },
		"blank source": func(s string) string {
			return strings.Replace(s, `"source":"Удаление индекса"`, `"source":" "`, 1)
		},
		"missing quote":    func(s string) string { return strings.Replace(s, `,"sourceQuote":""`, "", 1) },
		"non-string quote": func(s string) string { return strings.Replace(s, `"sourceQuote":""`, `"sourceQuote":42`, 1) },
		"null quote":       func(s string) string { return strings.Replace(s, `"sourceQuote":""`, `"sourceQuote":null`, 1) },
		"old format":       func(string) string { return `{"issues":[],"goals":[],"cards":[]}` },
		"unknown kind": func(s string) string {
			return strings.Replace(s, `"extraCards":[]`, `"extraCards":[{"kind":"other"}]`, 1)
		},
		"null card": func(s string) string { return strings.Replace(s, `"extraCards":[]`, `"extraCards":[null]`, 1) },
	} {
		t.Run(defect, func(t *testing.T) {
			b := mutate(directReply())
			calls := 0
			p := providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
				if req.System == imageDiscoveryPrompt {
					return providers.Response{Content: []byte(`{"figures":[]}`)}, nil
				}
				calls++
				return providers.Response{Content: []byte(b), Truncated: defect == "truncated flag"}, nil
			})
			r, err := NewGenerator(p, Options{OnBatch: func(Batch) { t.Fatal("published invalid response") }}).Generate(context.Background(), pdfDocument())
			if err == nil || calls != 1 || len(r.Facts) != 0 || r.Stats.InvalidResponses != 1 {
				t.Fatalf("%+v %v calls=%d", r, err, calls)
			}
		})
	}
}
func TestDirectGenerationKeepsIssuesAndFailureMetrics(t *testing.T) {
	p := providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
		if req.System == imageDiscoveryPrompt {
			return providers.Response{Content: []byte(`{"figures":[]}`)}, nil
		}
		return providers.Response{Content: []byte(strings.Replace(directReply(), `"issues":[]`, `"issues":["Page 1 contains an unreadable fragment"]`, 1))}, nil
	})
	r, err := NewGenerator(p, Options{}).Generate(context.Background(), pdfDocument())
	if err != nil || CardCount(r.Facts) != 2 || len(r.Coverage.Issues) != 1 {
		t.Fatalf("%+v %v", r, err)
	}
	p = providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
		if req.System == imageDiscoveryPrompt {
			return providers.Response{Content: []byte(`{"figures":[]}`)}, nil
		}
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
		p := providerFunc(func(ctx context.Context, req providers.Request) (providers.Response, error) {
			if req.System == imageDiscoveryPrompt {
				return providers.Response{Content: []byte(`{"figures":[]}`)}, nil
			}
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
		} else if !errors.Is(e, providers.ErrBudget) || calls != 0 || CardCount(r.Facts) != 0 || r.Budget.CommittedUSD != .03 {
			t.Fatalf("%+v %v", r, e)
		}
	}
}
func TestDirectGenerationRejectsFactLimitWithoutPartialPublication(t *testing.T) {
	r, e := NewGenerator(stageFixture(learningFact("A"), learningFact("B")), Options{MaxFactsPerDoc: 1, OnBatch: func(Batch) { t.Fatal("published over limit") }}).Generate(context.Background(), pdfDocument())
	if e == nil || len(r.Facts) != 0 {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestInvalidLastFactRejectsWholeResponse(t *testing.T) {
	a, b := learningFact("A"), learningFact("B")
	b.Cards[0] = FlipCard{cardBase("  a?  "), "answer"}
	r, err := NewGenerator(stageFixture(a, b), Options{OnBatch: func(Batch) { t.Fatal("published before validating all facts") }}).Generate(t.Context(), Document{Text: "A and B"})
	if err == nil || len(r.Facts) != 0 || r.Stats.InvalidResponses != 1 {
		t.Fatalf("%+v %v", r, err)
	}
}

func TestInvalidLastRecallReportsOriginalPath(t *testing.T) {
	raw := `{"issues":[],"facts":[` + testFact + `,` + strings.Replace(strings.Replace(testFact, `"name":"Удаление"`, `"name":"Другой факт"`, 1), `"answer":"Удалить запись индекса; записать блок; пометить данные."`, `"answer":" "`, 1) + `]}`
	p := providerFunc(func(_ context.Context, _ providers.Request) (providers.Response, error) {
		return providers.Response{Content: []byte(raw)}, nil
	})
	r, err := NewGenerator(p, Options{OnBatch: func(Batch) { t.Fatal("published partial set") }}).Generate(t.Context(), Document{Text: "source"})
	if err == nil || len(r.Facts) != 0 || len(r.Artifact.Facts) != 0 {
		t.Fatalf("%+v %v", r, err)
	}
	if fields := diagnosticFields(t, err); fields["path"] != "facts[1].recall.answer" {
		t.Fatalf("%v", fields)
	}
}
