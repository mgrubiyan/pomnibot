package generator

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/aistudio"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/gigachat"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/yandexgpt"
)

const privateDiagnosticText = "PRIVATE_DOCUMENT_MODEL_TEXT"

func diagnosticFields(t *testing.T, err error) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	slog.New(slog.NewJSONHandler(&buf, nil)).Error("failure", "error", err, "generation", Diagnostics(fmt.Errorf("wrapped: %w", err)))
	if strings.Contains(buf.String(), privateDiagnosticText) {
		t.Fatalf("private content in diagnostics: %s", &buf)
	}
	var record map[string]any
	if e := json.Unmarshal(buf.Bytes(), &record); e != nil {
		t.Fatal(e)
	}
	fields, ok := record["generation"].(map[string]any)
	if !ok {
		t.Fatalf("missing diagnostics: %s", &buf)
	}
	return fields
}

func TestResponseFailureDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name, content, category, path, reason string
		truncated                             bool
	}{
		{name: "truncated", content: directReply(), category: "truncated", reason: "response was truncated", truncated: true},
		{name: "empty", category: "empty_response", reason: "response is empty"},
		{name: "whitespace", content: " \n\t", category: "empty_response", reason: "response is empty"},
		{name: "JSON", content: `{"` + privateDiagnosticText + `":!}`, category: "invalid_json", reason: "invalid JSON"},
		{name: "trailing JSON", content: directReply() + `{}`, category: "invalid_json", reason: "invalid JSON"},
		{name: "root type", content: `[]`, category: "invalid_structure", reason: "expected object"},
		{name: "missing", content: `{"issues":[]}`, category: "missing_field", path: "facts", reason: "required field is missing"},
		{name: "null", content: `{"issues":[],"facts":null}`, category: "null_field", path: "facts", reason: "required field is null"},
		{name: "unknown", content: strings.Replace(directReply(), `"issues":[]`, `"issues":[],"`+privateDiagnosticText+`":1`, 1), category: "invalid_structure", path: "<unknown>", reason: "unknown field"},
		{name: "fact type", content: strings.Replace(directReply(), `"pages":[1]`, `"pages":"`+privateDiagnosticText+`"`, 1), category: "invalid_structure", path: "facts[0].pages", reason: "expected array"},
		{name: "fact unknown", content: strings.Replace(directReply(), `"pages":[1]`, `"pages":[1],"`+privateDiagnosticText+`":1`, 1), category: "invalid_structure", path: "facts[0].<unknown>", reason: "unknown field"},
		{name: "recall missing", content: strings.Replace(directReply(), `"recall":`, `"other":`, 1), category: "missing_field", path: "facts[0].recall", reason: "required field is missing"},
		{name: "recall null", content: strings.Replace(directReply(), `"answer":"Удалить запись индекса; записать блок; пометить данные."`, `"answer":null`, 1), category: "null_field", path: "facts[0].recall.answer", reason: "required field is null"},
		{name: "recall type", content: strings.Replace(directReply(), `"answer":"Удалить запись индекса; записать блок; пометить данные."`, `"answer":42`, 1), category: "invalid_structure", path: "facts[0].recall.answer", reason: "expected string"},
		{name: "extra kind", content: strings.Replace(directReply(), `"extraCards":[]`, `"extraCards":[{"kind":"`+privateDiagnosticText+`"}]`, 1), category: "invalid_structure", path: "facts[0].extraCards[0].kind", reason: "unknown card kind"},
		{name: "empty facts", content: `{"issues":[],"facts":[]}`, category: "validation", path: "facts", reason: "facts are empty"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
				return providers.Response{Content: []byte(tc.content), Model: "test-model", Truncated: tc.truncated, Usage: providers.Usage{Attempts: 2, FailedAttempts: 1, PromptTokens: 20, CompletionTokens: 10, ReasoningTokens: new(3), CostUSD: .02}}, nil
			})
			r, err := NewGenerator(p, Options{}).Generate(t.Context(), Document{Text: privateDiagnosticText})
			if err == nil {
				t.Fatal("accepted invalid response")
			}
			fields := diagnosticFields(t, err)
			for key, want := range map[string]any{"stage": "generation", "category": tc.category, "model": "test-model", "truncated": tc.truncated, "response_bytes": float64(len(tc.content)), "max_tokens": float64(128000), "reasoning_effort": "medium", "attempts": float64(2), "prompt_tokens": float64(20), "completion_tokens": float64(10), "reasoning_tokens": float64(3)} {
				if fields[key] != want {
					t.Errorf("%s = %v, want %v", key, fields[key], want)
				}
			}
			if tc.path != "" && fields["path"] != tc.path {
				t.Errorf("path = %v, want %s", fields["path"], tc.path)
			}
			if !strings.Contains(err.Error(), tc.reason) {
				t.Errorf("error = %v, want reason %q", err, tc.reason)
			}
			if tc.category == "invalid_json" && fields["json_offset"] == nil {
				t.Error("missing JSON position")
			}
			if fields["duration"] == nil || r.Stats.Stages["generation"].Error != err.Error() || r.Stats.InvalidResponses != 1 || r.Stats.CostUSD != .02 {
				t.Fatalf("inconsistent stats: %+v, %v", r.Stats, err)
			}
		})
	}
}

func TestProviderFailureDiagnosticsPreserveCause(t *testing.T) {
	for _, tc := range []struct {
		category string
		cause    error
	}{
		{"timeout", context.DeadlineExceeded}, {"canceled", context.Canceled}, {"budget", providers.ErrBudget}, {"refused", providers.ErrRefused}, {"capability", providers.ErrCapability}, {"provider", errors.New(privateDiagnosticText)},
		{"busy", providers.ErrBusy}, {"unavailable", providers.ErrUnavailable},
	} {
		t.Run(tc.category, func(t *testing.T) {
			p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
				return providers.Response{}, fmt.Errorf("%s: %w", privateDiagnosticText, tc.cause)
			})
			r, err := NewGenerator(p, Options{}).Generate(t.Context(), Document{Text: privateDiagnosticText})
			if !errors.Is(err, tc.cause) {
				t.Fatalf("lost cause: %v", err)
			}
			fields := diagnosticFields(t, err)
			if fields["category"] != tc.category || fields["stage"] != "generation" || r.Stats.Stages["generation"].Error != err.Error() {
				t.Fatalf("%v: %v", fields, err)
			}
			for _, key := range []string{"model", "attempts", "prompt_tokens", "completion_tokens", "reasoning_tokens", "response_bytes", "truncated"} {
				if _, ok := fields[key]; ok {
					t.Errorf("unknown %s reported: %v", key, fields[key])
				}
			}
		})
	}
}

func TestGenerationCallTimeoutAndCancellation(t *testing.T) {
	for _, canceled := range []bool{false, true} {
		ctx, cancel := context.WithCancel(t.Context())
		if canceled {
			cancel()
		}
		p := providerFunc(func(ctx context.Context, _ providers.Request) (providers.Response, error) {
			<-ctx.Done()
			return providers.Response{}, ctx.Err()
		})
		_, err := NewGenerator(p, Options{CallTimeout: time.Millisecond}).Generate(ctx, Document{Text: "source"})
		cancel()
		want := context.DeadlineExceeded
		if canceled {
			want = context.Canceled
		}
		if !errors.Is(err, want) {
			t.Fatalf("lost context cause: %v", err)
		}
	}
}

func TestValidationDiagnosticsUseWireIndicesWithoutPrivateValues(t *testing.T) {
	for _, tc := range []struct {
		name, path, reason string
		mutate             func(*Fact)
	}{
		{"blank name", "name", "fact name is empty", func(f *Fact) { f.Name = " " }},
		{"empty pages", "pages", "fact pages are empty", func(f *Fact) { f.Pages = []int{} }},
		{"outside pages", "pages[0]", "page is outside document range 1..1", func(f *Fact) { f.Pages = []int{2} }},
		{"duplicate pages", "pages[1]", "duplicate fact page", func(f *Fact) { f.Pages = []int{1, 1} }},
		{"blank question", "recall.question", "question is empty", func(f *Fact) { v := f.Cards[4].(FlipCard); v.Question = " "; f.Cards[4] = v }},
		{"blank source", "extraCards[2].source", "source is empty", func(f *Fact) { v := f.Cards[3].(BooleanCard); v.Source = " "; f.Cards[3] = v }},
		{"blank input", "recall.answer", "answer is empty", func(f *Fact) { v := f.Cards[4].(FlipCard); v.Answer = " "; f.Cards[4] = v }},
		{"blank flip", "recall.answer", "answer is empty", func(f *Fact) { v := f.Cards[4].(FlipCard); v.Answer = " "; f.Cards[4] = v }},
		{"missing right answers", "extraCards[1].rightAnswers", "right answers are empty", func(f *Fact) { v := f.Cards[2].(ChoiceCard); v.RightAnswers = []string{}; f.Cards[2] = v }},
		{"missing wrong answers", "extraCards[1].wrongAnswers", "wrong answers are empty", func(f *Fact) { v := f.Cards[2].(ChoiceCard); v.WrongAnswers = []string{}; f.Cards[2] = v }},
		{"blank choice", "extraCards[1].rightAnswers[1]", "choice answer is empty", func(f *Fact) { v := f.Cards[2].(ChoiceCard); v.RightAnswers[1] = " "; f.Cards[2] = v }},
		{"duplicate choice", "extraCards[1].wrongAnswers[0]", "duplicate choice answer", func(f *Fact) {
			v := f.Cards[2].(ChoiceCard)
			v.RightAnswers[0] = privateDiagnosticText
			v.WrongAnswers[0] = privateDiagnosticText
			f.Cards[2] = v
		}},
		{"short table", "extraCards[0].columns", "table requires at least two categories", func(f *Fact) { v := f.Cards[1].(TableCard); v.Columns = v.Columns[:1]; f.Cards[1] = v }},
		{"blank category", "extraCards[0].columns[0].name", "table category name is empty", func(f *Fact) { v := f.Cards[1].(TableCard); v.Columns[0].Name = " "; f.Cards[1] = v }},
		{"duplicate category", "extraCards[0].columns[1].name", "duplicate table category", func(f *Fact) {
			v := f.Cards[1].(TableCard)
			v.Columns[0].Name = privateDiagnosticText
			v.Columns[1].Name = privateDiagnosticText
			f.Cards[1] = v
		}},
		{"empty rows", "extraCards[0].columns[1].rows", "table rows are empty", func(f *Fact) { v := f.Cards[1].(TableCard); v.Columns[1].Rows = nil; f.Cards[1] = v }},
		{"blank row", "extraCards[0].columns[1].rows[0]", "table row is empty", func(f *Fact) { v := f.Cards[1].(TableCard); v.Columns[1].Rows[0] = " "; f.Cards[1] = v }},
		{"duplicate row", "extraCards[0].columns[1].rows[0]", "duplicate table row", func(f *Fact) {
			v := f.Cards[1].(TableCard)
			v.Columns[0].Rows[0] = privateDiagnosticText
			v.Columns[1].Rows[0] = privateDiagnosticText
			f.Cards[1] = v
		}},
		{"duplicate question", "extraCards[3].question", "duplicate card question of the same kind", func(f *Fact) { f.Cards = append(f.Cards, f.Cards[4]) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fact := requiredFormatsFixture()
			fact.Name = privateDiagnosticText
			tc.mutate(&fact)
			r, err := NewGenerator(stageFixture(fact), Options{}).Generate(t.Context(), Document{Text: privateDiagnosticText})
			if err == nil {
				t.Fatal("expected validation error")
			}
			fields := diagnosticFields(t, err)
			if fields["category"] != "validation" || fields["path"] != "facts[0]."+tc.path || fields["reason"] != tc.reason {
				t.Errorf("%v", fields)
			}
			if r.Stats.Stages["generation"].Error != err.Error() || r.Stats.InvalidResponses != 1 {
				t.Errorf("inconsistent stats: %+v", r.Stats)
			}
		})
	}
}

func TestNestedResponseDiagnosticsAndParserCauses(t *testing.T) {
	b, err := marshalModelFixture(requiredFormatsFixture())
	if err != nil {
		t.Fatal(err)
	}
	// Go 1.27 adds array indices to UnmarshalTypeError.Field. Go 1.26
	// exposes the same field path without those optional nested indices.
	for _, tc := range []struct{ content, path, indexedPath string }{
		{strings.Replace(string(b), `"rows":["B"]`, `"rows":["B",42]`, 1), "facts[0].extraCards[0].columns.rows", "facts[0].extraCards[0].columns[1].rows[1]"},
		{strings.Replace(string(b), `"rows":["B"]`, `"rows":["B"],"`+privateDiagnosticText+`":1`, 1), "facts[0].extraCards[0].<unknown>", ""},
		{strings.Replace(string(b), `"issues":[]`, `"issues":["ok",42]`, 1), "issues", "issues[1]"},
	} {
		p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
			return providers.Response{Content: []byte(tc.content)}, nil
		})
		_, err := NewGenerator(p, Options{}).Generate(t.Context(), Document{Text: "source"})
		if err == nil {
			t.Fatal("accepted invalid structure")
		}
		if fields := diagnosticFields(t, err); fields["path"] != tc.path && (tc.indexedPath == "" || fields["path"] != tc.indexedPath) {
			t.Errorf("path = %v, want %s", fields["path"], tc.path)
		}
		if !strings.Contains(tc.path, "<unknown>") {
			var typeErr *json.UnmarshalTypeError
			if !errors.As(err, &typeErr) {
				t.Errorf("lost parser cause: %v", err)
			}
		}
	}
	_, err = NewGenerator(providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
		return providers.Response{Content: []byte(`{"facts":!}`)}, nil
	}), Options{}).Generate(t.Context(), Document{Text: "source"})
	var syntax *json.SyntaxError
	if !errors.As(err, &syntax) {
		t.Fatalf("lost syntax error: %v", err)
	}
	if fields := diagnosticFields(t, err); fields["json_offset"] != float64(syntax.Offset) {
		t.Errorf("incorrect syntax position: %v", fields)
	}
}

func TestPreflightAndFactLimitDiagnostics(t *testing.T) {
	for _, tc := range []struct {
		name           string
		doc            Document
		opts           Options
		path, category string
	}{
		{"empty document", Document{}, Options{}, "", "preflight"},
		{"PDF", Document{PDFs: []providers.PDF{{Name: privateDiagnosticText}}}, Options{}, "pdfs[0].pages", "preflight"},
		{"empty PDF data", Document{PDFs: []providers.PDF{{Name: privateDiagnosticText, Pages: 1}}}, Options{}, "pdfs[0].data", "preflight"},
		{"invalid output limit", Document{Text: "source"}, Options{MaxTokens: -1}, "maxTokens", "configuration"},
		{"invalid reasoning", Document{Text: "source"}, Options{ReasoningEffort: privateDiagnosticText}, "reasoningEffort", "configuration"},
		{"offsets", Document{Text: "text", PageStarts: []int{0, 99}}, Options{}, "pageStarts[1]", "preflight"},
		{"source limit", Document{Text: privateDiagnosticText}, Options{SourceBudget: 1}, "source", "limit"},
		{"fact limit", Document{Text: "source"}, Options{MaxFactsPerDoc: 1}, "facts", "limit"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			p := providerFunc(func(ctx context.Context, req providers.Request) (providers.Response, error) {
				calls++
				return stageFixture(learningFact("A"), learningFact("B")).Complete(ctx, req)
			})
			r, err := NewGenerator(p, tc.opts).Generate(t.Context(), tc.doc)
			if err == nil {
				t.Fatal("expected failure")
			}
			fields := diagnosticFields(t, err)
			stage := "preflight"
			if tc.name == "fact limit" {
				stage = "generation"
				if r.Stats.Stages[stage].Error != err.Error() {
					t.Error("inconsistent validation error")
				}
			} else if calls != 0 {
				t.Error("called provider after failed preflight")
			}
			if fields["stage"] != stage || fields["category"] != tc.category {
				t.Fatalf("%v", fields)
			}
			if tc.path != "" && fields["path"] != tc.path {
				t.Errorf("path = %v, want %s", fields["path"], tc.path)
			}
			if tc.name == "source limit" && !errors.Is(err, providers.ErrCapability) {
				t.Error("lost capability sentinel")
			}
			if stage == "preflight" {
				for _, key := range []string{"response_bytes", "truncated", "attempts", "prompt_tokens"} {
					if _, ok := fields[key]; ok {
						t.Errorf("preflight has %s", key)
					}
				}
			}
		})
	}
}

func TestProviderHTTPDiagnosticsOmitBodyAndPreserveTypedCause(t *testing.T) {
	for _, cause := range []error{
		&openrouter.APIError{Status: 429},
		&gigachat.APIError{Status: 429, Body: privateDiagnosticText},
		&yandexgpt.APIError{Status: 429, Body: privateDiagnosticText},
		&aistudio.APIError{Status: 429, Body: privateDiagnosticText},
	} {
		t.Run(fmt.Sprintf("%T", cause), func(t *testing.T) {
			p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
				return providers.Response{Model: "test-model", Usage: providers.Usage{Attempts: 3, FailedAttempts: 3, UnreportedCalls: 2}}, fmt.Errorf("%s: %w", privateDiagnosticText, errors.Join(providers.ErrUnavailable, cause))
			})
			r, err := NewGenerator(p, Options{}).Generate(t.Context(), Document{Text: "source"})
			if !errors.Is(err, cause) || !errors.Is(err, providers.ErrUnavailable) {
				t.Fatalf("lost cause: %v", err)
			}
			fields := diagnosticFields(t, err)
			if fields["provider_status"] != float64(429) || fields["category"] != "unavailable" || fields["attempts"] != float64(3) || fields["failed_attempts"] != float64(3) || fields["unreported_usage_calls"] != float64(2) || !strings.Contains(err.Error(), "HTTP status 429") {
				t.Fatalf("%v: %v", fields, err)
			}
			if r.Stats.ModelCalls != 3 || r.Stats.FailedCalls != 3 || r.Stats.UnreportedUsageCalls != 2 {
				t.Fatalf("lost accounting: %+v", r.Stats)
			}
		})
	}
}

func TestDiagnosticOptionalUsageAndSuccessfulAccounting(t *testing.T) {
	for _, invalid := range []bool{false, true} {
		p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
			content := directReply()
			if invalid {
				content = ""
			}
			return providers.Response{Content: []byte(content), Model: "test-model", Usage: providers.Usage{Attempts: 2, FailedAttempts: 1, PromptTokens: 20, CompletionTokens: 10, ReasoningTokens: new(0), CachedTokens: new(0), CacheWriteTokens: new(5), CostUSD: .02}}, nil
		})
		r, err := NewGenerator(p, Options{}).Generate(t.Context(), Document{Text: "source"})
		if invalid {
			if err == nil {
				t.Fatal("expected error")
			}
			fields := diagnosticFields(t, err)
			for key, want := range map[string]any{"reasoning_tokens": float64(0), "cached_tokens": float64(0), "cache_write_tokens": float64(5)} {
				if fields[key] != want {
					t.Errorf("%s = %v, want %v", key, fields[key], want)
				}
			}
		} else if err != nil || r.Stats.Stages["generation"].Error != "" || CardCount(r.Facts) != 2 || r.Stats.InvalidResponses != 0 {
			t.Fatalf("successful generation changed: %+v, %v", r, err)
		}
		if r.Stats.ModelCalls != 2 || r.Stats.FailedCalls != 1 || r.Stats.CostUSD != .02 || r.Stats.PromptTokens != 20 || r.Stats.CompletionTokens != 10 || r.Stats.ReasoningTokens == nil || *r.Stats.ReasoningTokens != 0 || !r.Stats.CacheUsageReported || r.Stats.CacheWriteTokens != 5 {
			t.Fatalf("lost accounting: %+v", r.Stats)
		}
	}
}

func TestResponseDiagnosticsPreserveDuplicateFieldDecoding(t *testing.T) {
	invalidFact := strings.Replace(testFact, `"extraCards":[]`, `"extraCards":[{"kind":"`+privateDiagnosticText+`"}]`, 1)
	for _, tc := range []struct {
		name, content, path string
		valid               bool
	}{
		{"valid duplicate facts", `{"issues":[],"facts":[` + testFact + `],"facts":[` + testFact + `]}`, "", true},
		{"invalid earlier facts", `{"issues":[],"facts":[` + invalidFact + `],"facts":[` + testFact + `]}`, "facts[0].extraCards[0].kind", false},
		{"invalid earlier name", strings.Replace(directReply(), `"name":"Удаление"`, `"name":42,"name":"Удаление"`, 1), "facts[0].name", false},
		{"invalid earlier issues", strings.Replace(directReply(), `"issues":[]`, `"issues":42,"issues":[]`, 1), "issues", false},
		{"null issues alias", strings.Replace(directReply(), `"issues":[]`, `"issues":[],"ISSUES":null`, 1), "issues", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := providerFunc(func(context.Context, providers.Request) (providers.Response, error) {
				return providers.Response{Content: []byte(tc.content)}, nil
			})
			_, err := NewGenerator(p, Options{}).Generate(t.Context(), Document{Text: "source"})
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil {
				t.Fatal("accepted invalid response")
			}
			if fields := diagnosticFields(t, err); fields["path"] != tc.path {
				t.Errorf("path = %v, want %s", fields["path"], tc.path)
			}
		})
	}
}

func TestTypeDiagnosticsOmitEmbeddedGoStructNames(t *testing.T) {
	// Go 1.26 exposes CardBase in UnmarshalTypeError.Field for promoted fields;
	// Go 1.27 exposes the JSON field directly. Neither shape changes the wire path.
	for _, field := range []string{"CardBase.question", "question", "CardBase.source", "source", "CardBase.sourceQuote", "sourceQuote"} {
		t.Run(field, func(t *testing.T) {
			cause := &json.UnmarshalTypeError{Value: "bool", Type: reflect.TypeFor[string](), Field: field}
			err := atPath("facts[0].recall", safeDecodeProblem(cause))
			want := "facts[0].recall." + strings.TrimPrefix(field, "CardBase.")
			if fields := diagnosticFields(t, err); fields["path"] != want {
				t.Fatalf("%v, want path %s", fields, want)
			}
			var got *json.UnmarshalTypeError
			if !errors.As(err, &got) || got != cause {
				t.Fatal("lost parser cause")
			}
		})
	}
}
