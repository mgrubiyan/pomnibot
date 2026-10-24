package generator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

const testSVG = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 100 100"><path d="M0 0L100 100" stroke="black"/></svg>`

func TestGenerationPreservesGFMAndMath(t *testing.T) {
	question := "**Вычислите** $\\frac{a_{n+1}}{2}$\n\n| a | n |\n|---|---|\n| 4 | 1 |"
	answer := `$$\frac{4}{2} = 2$$`
	quote := "  Исходник: aₙ₊₁/2 = 2.\nБез изменений.  "
	f := learningFact("Формула")
	b := CardBase{Question: question, Source: "Пояснение: " + answer, SourceQuote: quote}
	f.Cards = []Card{FlipCard{b, answer}, InputCard{b, answer}, ChoiceCard{CardBase: cardBase("Выберите $x$"), RightAnswers: []string{`$\alpha$`}, WrongAnswers: []string{`$\beta$`}}, TableCard{CardBase: cardBase("Распределите"), Columns: []Column{{Name: "**A**", Rows: []string{`$\frac{1}{2}$`}}, {Name: "B", Rows: []string{"| a |\n|---|\n| b |"}}}}}
	r, err := NewGenerator(stageFixture(f), Options{}).Generate(t.Context(), Document{Text: quote})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Facts[0].Cards {
		switch v := c.(type) {
		case FlipCard:
			if v.Question != question || v.Answer != answer || v.SourceQuote != quote || v.Source != b.Source {
				t.Fatalf("changed markup: %+v", v)
			}
		case InputCard:
			if v.Question != question || v.Answer != answer || v.SourceQuote != quote {
				t.Fatalf("changed recall: %+v", v)
			}
		case ChoiceCard:
			if !reflect.DeepEqual(v.RightAnswers, []string{`$\alpha$`}) {
				t.Fatal("changed choice")
			}
		case TableCard:
			if !reflect.DeepEqual(v.Columns, f.Cards[3].(TableCard).Columns) {
				t.Fatal("changed sorting content")
			}
		}
	}
}

func TestIllustrationsAreBoundAfterValidationAndSharedByRecall(t *testing.T) {
	f := learningFact("Main")
	f.Cards = append(f.Cards, FlipCard{cardBase("Extra?"), "other"}, InputCard{cardBase("Extra?"), "other"})
	calls, published := 0, false
	p := providerFunc(func(ctx context.Context, req providers.Request) (providers.Response, error) {
		calls++
		if published {
			t.Fatal("published before enrichment")
		}
		if providers.BudgetFrom(ctx) == nil || req.ReasoningEffort != "high" || req.MaxTokens != 12345 || !req.NoFallback || !req.NoCache {
			t.Fatal("lost generation settings")
		}
		if calls == 1 {
			b, _ := marshalModelFixture(f)
			return providers.Response{Content: b, Model: "same", Usage: providers.Usage{Attempts: 1, PromptTokens: 10}}, nil
		}
		if req.Source != "" || len(req.PDFs) != 0 || strings.Contains(req.User, "PRIVATE_ORIGINAL") {
			t.Fatal("resent original document")
		}
		var input struct {
			Cards []struct {
				ID string `json:"id"`
			} `json:"cards"`
		}
		if err := json.Unmarshal([]byte(req.User), &input); err != nil || len(input.Cards) != 2 {
			t.Fatalf("must send each recall once: %s", req.User)
		}
		b, _ := json.Marshal(map[string]any{"illustrations": []any{
			map[string]any{"cardId": input.Cards[1].ID, "questionSvg": "", "explanationSvg": testSVG},
			map[string]any{"cardId": input.Cards[0].ID, "questionSvg": testSVG, "explanationSvg": ""},
		}})
		return providers.Response{Content: b, Model: "same", Usage: providers.Usage{Attempts: 1, PromptTokens: 20}}, nil
	})
	r, err := NewGenerator(p, Options{ReasoningEffort: "high", MaxTokens: 12345, OnBatch: func(b Batch) {
		published = true
		if b.Facts[0].Cards[0].Base().QuestionSvg != testSVG {
			t.Fatal("published without SVG")
		}
	}}).Generate(t.Context(), Document{Text: "PRIVATE_ORIGINAL"})
	if err != nil || calls != 2 || !published {
		t.Fatalf("calls=%d published=%v error=%v", calls, published, err)
	}
	for _, c := range r.Facts[0].Cards {
		b := c.Base()
		if b.Question == "Main?" && (b.QuestionSvg != testSVG || b.ExplanationSvg != "") {
			t.Fatalf("main: %+v", b)
		}
		if b.Question == "Extra?" && (b.QuestionSvg != "" || b.ExplanationSvg != testSVG) {
			t.Fatalf("extra: %+v", b)
		}
	}
	if r.Stats.ModelCalls != 2 || r.Stats.PromptTokens != 30 || len(r.Stats.Stages) != 2 || len(r.Artifact.Requests) != 2 || len(r.Artifact.Responses) != 2 {
		t.Fatalf("lost stage accounting: %+v", r)
	}
}

func TestIllustrationFailuresKeepValidatedCards(t *testing.T) {
	for _, tc := range []struct {
		name, content string
		err           error
		truncated     bool
	}{
		{name: "none", content: `{"illustrations":[]}`},
		{name: "provider", err: errors.New("private provider failure")},
		{name: "budget", err: providers.ErrBudget},
		{name: "malformed", content: `{"illustrations":`},
		{name: "wrong root", content: `{"facts":[]}`},
		{name: "truncated", content: `{"illustrations":[]}`, truncated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
				calls++
				if calls == 1 {
					b, _ := marshalModelFixture(learningFact("Fact"))
					return providers.Response{Content: b}, nil
				}
				return providers.Response{Content: []byte(tc.content), Truncated: tc.truncated}, tc.err
			})
			r, err := NewGenerator(p, Options{}).Generate(t.Context(), Document{Text: "source"})
			if err != nil || len(r.Facts) != 1 || calls != 2 || r.Facts[0].Cards[0].Base().QuestionSvg != "" {
				t.Fatalf("lost cards: %+v %v", r, err)
			}
			if tc.name != "none" && r.Stats.Stages["illustrations"].Error == "" {
				t.Fatal("missing diagnostics")
			}
		})
	}
}

func TestIllustrationPartialResponse(t *testing.T) {
	calls := 0
	p := providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			b, _ := marshalModelFixture(learningFact("Fact"), learningFact("Other"), learningFact("Third"))
			return providers.Response{Content: b}, nil
		}
		var input struct {
			Cards []struct {
				ID string `json:"id"`
			} `json:"cards"`
		}
		_ = json.Unmarshal([]byte(req.User), &input)
		b, _ := json.Marshal(map[string]any{"illustrations": []any{
			map[string]any{"cardId": input.Cards[0].ID, "questionSvg": `<svg viewBox="0 0 1 1" onload="alert(1)"/>`, "explanationSvg": testSVG},
			map[string]any{"cardId": input.Cards[1].ID, "questionSvg": testSVG, "explanationSvg": strings.Repeat("x", 128*1024+1)},
			map[string]any{"cardId": input.Cards[2].ID, "questionSvg": 42, "explanationSvg": testSVG},
			map[string]any{"cardId": input.Cards[0].ID, "questionSvg": testSVG, "explanationSvg": ""},
			map[string]any{"cardId": "unknown", "questionSvg": testSVG, "explanationSvg": ""},
			map[string]any{"cardId": input.Cards[0].ID, "question": "changed"},
			42,
		}})
		return providers.Response{Content: b}, nil
	})
	r, err := NewGenerator(p, Options{}).Generate(t.Context(), Document{Text: "source"})
	if err != nil {
		t.Fatal(err)
	}
	for i, fact := range r.Facts {
		for _, c := range fact.Cards {
			b := c.Base()
			if i == 0 && (b.QuestionSvg != "" || b.ExplanationSvg != testSVG || b.Question != "Fact?") {
				t.Fatalf("partial: %+v", b)
			}
			if i == 1 && (b.QuestionSvg != testSVG || b.ExplanationSvg != "") {
				t.Fatalf("partial: %+v", b)
			}
			if i == 2 && (b.QuestionSvg != "" || b.ExplanationSvg != testSVG) {
				t.Fatalf("invalid JSON value discarded its valid sibling: %+v", b)
			}
		}
	}
	if r.Stats.Stages["illustrations"].DroppedIllustrations < 2 {
		t.Fatal("missing invalid illustration count")
	}
}

func TestCancellationDuringIllustrationsDoesNotPublish(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	calls := 0
	p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			b, _ := marshalModelFixture(learningFact("Fact"))
			return providers.Response{Content: b}, nil
		}
		cancel()
		return providers.Response{}, ctx.Err()
	})
	_, err := NewGenerator(p, Options{OnBatch: func(Batch) { t.Fatal("published canceled job") }}).Generate(ctx, Document{Text: "source"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("lost cancellation: %v", err)
	}
}

func TestIllustrationTimeoutKeepsCards(t *testing.T) {
	calls := 0
	p := providerFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			b, _ := marshalModelFixture(learningFact("Fact"))
			return providers.Response{Content: b}, nil
		}
		<-ctx.Done()
		return providers.Response{}, ctx.Err()
	})
	r, err := NewGenerator(p, Options{CallTimeout: 10 * time.Millisecond}).Generate(t.Context(), Document{Text: "source"})
	if err != nil || CardCount(r.Facts) != 2 || r.Stats.Stages["illustrations"].Error == "" {
		t.Fatalf("timeout lost cards: %+v %v", r, err)
	}
}

func TestIllustrationsUseTheGenerationBackendForPDF(t *testing.T) {
	calls := 0
	router := providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			b, _ := marshalModelFixture(learningFact("Fact"))
			return providers.Response{Content: b}, nil
		}
		if req.Backend != "openrouter" || req.Source != "" || len(req.PDFs) != 0 {
			t.Fatal("lost backend pin")
		}
		return providers.Response{Content: []byte(`{"illustrations":[]}`)}, nil
	})
	primary := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		t.Fatal("illustrations switched to another provider")
		return providers.Response{}, nil
	})
	p := providers.NewFallback(time.Minute, providers.Backend{Name: "primary", Provider: primary}, providers.Backend{Name: "openrouter", Provider: router})
	r, err := NewGenerator(p, Options{}).Generate(t.Context(), pdfDocument())
	if err != nil || calls != 2 || r.Stats.Stages["illustrations"].Backend != "openrouter" {
		t.Fatalf("backend: %+v %v", r, err)
	}
}

func TestIllustrationsBindAllCardKinds(t *testing.T) {
	calls := 0
	expected := map[string]string{}
	p := providerFunc(func(_ context.Context, req providers.Request) (providers.Response, error) {
		calls++
		if calls == 1 {
			b, _ := marshalModelFixture(requiredFormatsFixture())
			return providers.Response{Content: b}, nil
		}
		var input struct {
			Cards []struct {
				ID   string `json:"id"`
				Card struct {
					Question string `json:"question"`
				} `json:"card"`
			} `json:"cards"`
		}
		if err := json.Unmarshal([]byte(req.User), &input); err != nil || len(input.Cards) != 4 {
			t.Fatalf("lost cards: %s", req.User)
		}
		bindings := []map[string]any{}
		for i, entry := range input.Cards {
			svg := strings.Replace(testSVG, "100 100", fmt.Sprintf("%d %d", i+1, i+2), 1)
			expected[entry.Card.Question] = svg
			bindings = append(bindings, map[string]any{"cardId": entry.ID, "questionSvg": svg, "explanationSvg": svg})
		}
		b, _ := json.Marshal(map[string]any{"illustrations": bindings})
		return providers.Response{Content: b}, nil
	})
	r, err := NewGenerator(p, Options{}).Generate(t.Context(), Document{Text: "source"})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range r.Facts[0].Cards {
		b := c.Base()
		if b.QuestionSvg != expected[b.Question] || b.ExplanationSvg != expected[b.Question] || b.QuestionSvg == "" {
			t.Fatalf("wrong SVG: %+v", b)
		}
	}
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	var stored Result
	if err := json.Unmarshal(data, &stored); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stored.Facts[0].Cards, r.Facts[0].Cards) {
		t.Fatal("report lost illustrations")
	}
}

func TestGenerationDoesNotAcceptIllustrationsInFirstResponse(t *testing.T) {
	for _, field := range []string{"questionSvg", "EXPLANATIONSVG"} {
		for _, extra := range []bool{false, true} {
			raw := directReply()
			if extra {
				raw = strings.Replace(raw, `"extraCards":[]`, `"extraCards":[{"kind":"boolean","question":"True?","source":"S","sourceQuote":"","answer":true,"`+field+`":"unsafe"}]`, 1)
			} else {
				raw = strings.Replace(raw, `"recall":{`, `"recall":{"`+field+`":"unsafe",`, 1)
			}
			p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
				return providers.Response{Content: []byte(raw)}, nil
			})
			if _, err := NewGenerator(p, Options{}).Generate(t.Context(), Document{Text: "source"}); err == nil {
				t.Fatalf("accepted first-stage %s", field)
			}
		}
	}
}
