package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/grader"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

func clientFor(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c, err := New(Config{APIKey: "secret", Model: DefaultModel, BaseURL: srv.URL, MaxTokens: 8000, HTTPClient: srv.Client()})
	if err != nil {
		t.Fatal(err)
	}
	c.backoff = time.Millisecond
	return c
}

func TestCompleteTextAndImage(t *testing.T) {
	for _, image := range []bool{false, true} {
		t.Run(fmt.Sprint(image), func(t *testing.T) {
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				if r.Method != http.MethodPost || r.URL.Path != "/chat/completions" || r.Header.Get("Authorization") != "Bearer secret" {
					t.Errorf("wrong request: %s %s", r.Method, r.URL.Path)
				}
				var body struct {
					Model     string `json:"model"`
					MaxTokens int    `json:"max_tokens"`
					Messages  []struct {
						Content json.RawMessage `json:"content"`
					} `json:"messages"`
					ResponseFormat struct {
						Type       string `json:"type"`
						JSONSchema struct {
							Strict bool `json:"strict"`
						} `json:"json_schema"`
					} `json:"response_format"`
					Reasoning map[string]any `json:"reasoning"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Model != DefaultModel || body.MaxTokens != 8000 || body.ResponseFormat.Type != "json_schema" || !body.ResponseFormat.JSONSchema.Strict || len(body.Messages) != 2 {
					t.Fatalf("body = %+v", body)
				}
				if enabled, ok := body.Reasoning["enabled"]; !ok || enabled != false || len(body.Reasoning) != 1 {
					t.Fatalf("thinking must be explicitly disabled: %+v", body.Reasoning)
				}
				if image {
					if !strings.Contains(string(body.Messages[1].Content), "data:image/png;base64,cG5n") {
						t.Error("missing image data")
					}
				} else if string(body.Messages[1].Content) != `"Конспект"` {
					t.Error("missing text")
				}
				_, _ = fmt.Fprint(w, `{"model":"actual/model","choices":[{"message":{"content":"Ответ"},"finish_reason":"length"}],"usage":{"prompt_tokens":12,"completion_tokens":34,"cost":0.001}}`)
			})
			req := providers.Request{System: "Правила", User: "Конспект", Schema: json.RawMessage(`{"type":"object"}`)}
			if image {
				req.Image = &providers.Image{Data: []byte("png"), MimeType: "image/png"}
			}
			res, err := c.Complete(context.Background(), req)
			if err != nil {
				t.Fatal(err)
			}
			if string(res.Content) != "Ответ" || res.Model != "actual/model" || !res.Truncated || res.Usage.PromptTokens != 12 || res.Usage.CompletionTokens != 34 {
				t.Fatalf("response = %+v", res)
			}
		})
	}
}

func TestCompleteTemperatureCompatibility(t *testing.T) {
	for _, tc := range []struct {
		name, model string
		temperature float64
		wantSent    bool
	}{
		{"haiku zero", DefaultModel, 0, false},
		{"haiku nonzero", DefaultModel, 0.3, false},
		{"haiku variant", DefaultModel + ":nitro", 0.3, false},
		{"haiku latest", "~anthropic/claude-haiku-latest", 0.3, false},
		{"haiku latest variant", "~anthropic/claude-haiku-latest:nitro", 0.3, false},
		{"legacy zero", "anthropic/claude-haiku-4.5", 0, true},
		{"legacy nonzero", "anthropic/claude-haiku-4.5", 0.3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				temperature, sent := body["temperature"]
				if sent != tc.wantSent {
					t.Errorf("temperature sent = %v, want %v", sent, tc.wantSent)
					w.WriteHeader(http.StatusNotFound)
					return
				}
				if sent {
					var got float64
					if err := json.Unmarshal(temperature, &got); err != nil || got != tc.temperature {
						t.Errorf("temperature = %s, want %v", temperature, tc.temperature)
					}
				}
				var provider struct {
					RequireParameters bool `json:"require_parameters"`
				}
				if err := json.Unmarshal(body["provider"], &provider); err != nil || !provider.RequireParameters {
					t.Errorf("provider must require parameter support: %s", body["provider"])
				}
				_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
			})
			c.cfg.Model = tc.model
			res, err := c.Complete(context.Background(), providers.Request{User: "Hello", Temperature: tc.temperature})
			if err != nil || string(res.Content) != "ok" {
				t.Fatalf("response = %+v, err = %v", res, err)
			}
		})
	}
}

func TestErrorsAndRetries(t *testing.T) {
	for _, tc := range []struct {
		status, calls int
		unavailable   bool
	}{
		{400, 1, false}, {401, 1, true}, {402, 1, true}, {403, 1, true}, {404, 1, true}, {429, 3, true}, {503, 3, true},
	} {
		t.Run(fmt.Sprint(tc.status), func(t *testing.T) {
			calls := 0
			c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
				calls++
				w.WriteHeader(tc.status)
				_, _ = fmt.Fprint(w, `{"error":{"message":"secret and private document"}}`)
			})
			_, err := c.Complete(context.Background(), providers.Request{User: "private document"})
			if err == nil || calls != tc.calls || errors.Is(err, providers.ErrUnavailable) != tc.unavailable {
				t.Fatalf("calls=%d, err=%v", calls, err)
			}
			if strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private document") {
				t.Fatalf("error exposes data: %v", err)
			}
		})
	}
}

func TestResponseFailures(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		refused    bool
	}{
		{"filter", `{"choices":[{"finish_reason":"content_filter","message":{"content":null}}]}`, true},
		{"refusal", `{"choices":[{"message":{"refusal":"no","content":null}}]}`, true},
		{"empty", `{"choices":[]}`, false},
		{"missing content", `{"choices":[{"message":{},"finish_reason":"stop"}]}`, false},
		{"invalid json", `private document`, false},
		{"embedded error", `{"error":{"code":503,"message":"private document"}}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) { _, _ = fmt.Fprint(w, tc.body) })
			_, err := c.Complete(context.Background(), providers.Request{})
			if err == nil || errors.Is(err, providers.ErrRefused) != tc.refused {
				t.Fatalf("err = %v", err)
			}
			if strings.Contains(err.Error(), "private document") {
				t.Fatal("response leaked")
			}
		})
	}
}

func TestRetryRecoversAndCancellationStopsWait(t *testing.T) {
	calls := 0
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(503)
			return
		}
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
	})
	res, err := c.Complete(context.Background(), providers.Request{})
	if err != nil || string(res.Content) != "ok" || calls != 2 || res.Model != DefaultModel {
		t.Fatalf("res=%+v calls=%d err=%v", res, calls, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	c = clientFor(t, func(w http.ResponseWriter, _ *http.Request) { cancel(); w.WriteHeader(429) })
	_, err = c.Complete(ctx, providers.Request{})
	if !errors.Is(err, context.Canceled) || errors.Is(err, providers.ErrUnavailable) {
		t.Fatalf("err=%v", err)
	}
}

func TestConfigFromEnv(t *testing.T) {
	for _, key := range []string{"OPENROUTER_KEY", "OPENROUTER_MODEL", "OPENROUTER_BASE_URL", "OPENROUTER_MAX_TOKENS", "OPENROUTER_EFFORT"} {
		t.Setenv(key, "")
	}
	if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "OPENROUTER_KEY") {
		t.Fatalf("err=%v", err)
	}
	t.Setenv("OPENROUTER_KEY", " secret ")
	cfg, err := ConfigFromEnv()
	if err != nil || cfg.APIKey != "secret" || cfg.Model != DefaultModel || cfg.BaseURL != DefaultBaseURL || cfg.MaxTokens != 50000 || cfg.Effort != "high" {
		t.Fatalf("cfg=%+v err=%v", cfg, err)
	}
	for _, tc := range []struct{ key, value string }{{"OPENROUTER_BASE_URL", "bad"}, {"OPENROUTER_BASE_URL", "ftp://host"}, {"OPENROUTER_MAX_TOKENS", "0"}, {"OPENROUTER_MAX_TOKENS", "many"}} {
		t.Run(tc.key+tc.value, func(t *testing.T) {
			t.Setenv(tc.key, tc.value)
			if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Fatalf("err=%v", err)
			}
		})
	}
}

func TestCompletePreservesMultipleSourceImages(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Messages []struct {
				Content json.RawMessage `json:"content"`
			} `json:"messages"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		var parts []part
		if len(body.Messages) != 2 {
			t.Error("messages lost")
		} else if err := json.Unmarshal(body.Messages[1].Content, &parts); err != nil || len(parts) != 3 {
			t.Errorf("image pages lost: %s", body.Messages[1].Content)
		}
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}]}`)
	})
	_, err := c.Complete(context.Background(), providers.Request{User: "pages 3 and 22", Images: []providers.Image{{MimeType: "image/png", Data: []byte("first")}, {MimeType: "image/jpeg", Data: []byte("second")}}})
	if err != nil {
		t.Fatal(err)
	}
}

func TestBudgetCountsTransportRetriesAndStopsBeforeSending(t *testing.T) {
	calls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls++; w.WriteHeader(http.StatusInternalServerError) }))
	defer server.Close()
	c, err := New(Config{APIKey: "test", Model: DefaultModel, BaseURL: server.URL, MaxTokens: 100})
	if err != nil {
		t.Fatal(err)
	}
	c.backoff = 0
	b := providers.NewBudget(providers.BudgetLimits{Calls: 1, USD: .01, CallUSD: .01})
	_, err = c.Complete(providers.WithBudget(context.Background(), b), providers.Request{User: "test"})
	if !errors.Is(err, providers.ErrBudget) || calls != 1 {
		t.Fatalf("retry escaped budget: calls=%d err=%v", calls, err)
	}
}

func TestBudgetRetainsReservationWhenUsageCostIsMissing(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":10}}`)
	})
	b := providers.NewBudget(providers.BudgetLimits{Calls: 2, USD: .06})
	_, err := c.Complete(providers.WithBudget(context.Background(), b), providers.Request{User: "test"})
	if err != nil {
		t.Fatal(err)
	}
	s := b.Snapshot()
	if s.CommittedUSD <= 0 || s.ReportedUSD != 0 || s.ReservedUSD != 0 {
		t.Fatalf("unreported cost treated as free: %+v", s)
	}
}

func TestOversizedInputDoesNotClaimDocumentBudgetExhaustion(t *testing.T) {
	calls := 0
	c := clientFor(t, func(http.ResponseWriter, *http.Request) { calls++ })
	budget := providers.NewBudget(providers.BudgetLimits{USD: .5})
	_, err := c.Complete(providers.WithBudget(context.Background(), budget), providers.Request{User: strings.Repeat("x", 100001)})
	if err == nil || errors.Is(err, providers.ErrBudget) || calls != 0 || budget.Snapshot().Calls != 0 {
		t.Fatalf("context cap mistaken for exhausted money: err=%v calls=%d budget=%+v", err, calls, budget.Snapshot())
	}
}

func TestGenerationPriceCeilingMatchesConservativeReservation(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Provider struct {
				MaxPrice map[string]float64 `json:"max_price"`
			} `json:"provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Provider.MaxPrice["prompt"] != .1 || body.Provider.MaxPrice["completion"] != .5 {
			t.Fatalf("wrong enforced price ceiling: %+v", body.Provider.MaxPrice)
		}
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}],"usage":{"cost":0.001}}`)
	})
	req := providers.Request{User: "whole source"}
	want := float64(c.inputBound(req))*.1/1e6 + 8000*.5/1e6
	if math.Abs(c.costBound(req)-want) > 1e-12 {
		t.Fatalf("reservation does not cover enforced prices: got=%g want=%g", c.costBound(req), want)
	}
	b := providers.NewBudget(providers.BudgetLimits{USD: .15, Calls: 1})
	if _, err := c.Complete(providers.WithBudget(context.Background(), b), req); err != nil {
		t.Fatal(err)
	}
}

func TestExplicitEffortEnablesThinkingAndKeepsUsageSeparate(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Reasoning struct {
				Enabled bool   `json:"enabled"`
				Effort  string `json:"effort"`
			} `json:"reasoning"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if !body.Reasoning.Enabled || body.Reasoning.Effort != "medium" {
			t.Fatalf("thinking not enabled: %+v", body.Reasoning)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"{}\"},\"finish_reason\":\"stop\"}],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20,\"completion_tokens_details\":{\"reasoning_tokens\":7},\"cost\":0.001}}\n\ndata: [DONE]\n\n")
	})
	c.cfg.Effort = "medium"
	b := providers.NewBudget(providers.BudgetLimits{USD: .15, Calls: 1})
	r, err := c.Complete(providers.WithBudget(context.Background(), b), providers.Request{})
	if err != nil || r.Usage.ReasoningTokens == nil || *r.Usage.ReasoningTokens != 7 || r.Usage.CompletionTokens != 20 || b.Snapshot().ReportedUSD != .001 {
		t.Fatalf("lost/double-counted thinking: %+v %v", r, err)
	}
}

func TestGradingDisablesDefaultThinking(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		var body chatRequest
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.Reasoning.Enabled || body.Reasoning.Effort != "" || body.MaxTokens != 256 {
			t.Errorf("grading inherited generation settings: %+v", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		content, _ := json.Marshal(`{"named":"упорядоченный массив","correct":true,"reason":"Верно."}`)
		_, _ = fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%s},\"finish_reason\":\"stop\"}]}\n\ndata: [DONE]\n\n", content)
	})
	c.cfg.Effort = "high"
	c.cfg.MaxTokens = 50000
	g := grader.Grader{Model: c}
	v := g.Check(context.Background(), grader.Input{
		Question: "На каком массиве применим бинарный поиск?",
		Expected: "на отсортированном массиве",
		Given:    "массив должен быть упорядочен",
	})
	if !v.Correct || v.Method != grader.MethodModel {
		t.Fatalf("grading failed: %+v", v)
	}
	if c.cfg.Effort != "high" || c.cfg.MaxTokens != 50000 {
		t.Fatal("grading changed shared provider defaults")
	}
}

func TestInvalidEffortFailsBeforeAnyRequest(t *testing.T) {
	_, err := New(Config{APIKey: "test", Model: DefaultModel, BaseURL: DefaultBaseURL, MaxTokens: 8000, Effort: "typo"})
	if err == nil {
		t.Fatal("unsupported effort accepted")
	}
}

func TestThinkingOnlyResponsePreservesTokensCostAndTruncation(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"model":"haiku","choices":[{"message":{"content":""},"finish_reason":"length"}],"usage":{"prompt_tokens":10,"completion_tokens":8000,"completion_tokens_details":{"reasoning_tokens":8000},"cost":0.004001}}`)
	})
	c.cfg.Effort = "medium"
	b := providers.NewBudget(providers.BudgetLimits{USD: .15, Calls: 1})
	r, err := c.Complete(providers.WithBudget(context.Background(), b), providers.Request{})
	if err == nil || !r.Truncated || r.Model != "haiku" || r.Usage.CompletionTokens != 8000 || r.Usage.ReasoningTokens == nil || *r.Usage.ReasoningTokens != 8000 || r.Usage.CostUSD != .004001 || len(r.Content) != 0 {
		t.Fatalf("lost thinking-only accounting: %+v %v", r, err)
	}
}

func TestPDFNativeCachingReasoningAndUsage(t *testing.T) {
	c := clientFor(t, func(w http.ResponseWriter, r *http.Request) {
		var b chatRequest
		if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
			t.Fatal(e)
		}
		raw, _ := json.Marshal(b)
		if len(b.Messages) != 3 || b.MaxTokens != 128000 || b.Reasoning.Effort != "medium" || !b.Reasoning.Enabled || !strings.Contains(string(raw), `"engine":"native"`) || !strings.Contains(string(raw), `"cache_control":{"type":"ephemeral"}`) || !strings.Contains(string(raw), "data:application/pdf;base64,") {
			t.Fatalf("bad PDF request %s", raw)
		}
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}],"usage":{"cost":0.01,"prompt_tokens_details":{"cached_tokens":500,"cache_write_tokens":2}}}`)
	})
	r, e := c.Complete(context.Background(), providers.Request{PDFs: []providers.PDF{{Name: "a.pdf", Data: []byte("%PDF"), Pages: 1}}, User: "suffix", ReasoningEffort: "medium", MaxTokens: 128000})
	if e != nil || r.Usage.CachedTokens == nil || *r.Usage.CachedTokens != 500 || r.Usage.Attempts != 1 {
		t.Fatalf("%+v %v", r, e)
	}
}

func TestPDFRetryAccountsForAllChargesAndCacheUsage(t *testing.T) {
	calls := 0
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		w.Header().Set("Content-Type", "text/event-stream")
		if calls == 1 {
			_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"partial\"}}],\"usage\":{\"cost\":0.002,\"prompt_tokens\":10,\"completion_tokens\":3,\"completion_tokens_details\":{\"reasoning_tokens\":2}}}\n\n")
			return
		}
		_, _ = fmt.Fprint(w, "data: {\"choices\":[{\"index\":0,\"delta\":{\"content\":\"{}\"},\"finish_reason\":\"stop\"}],\"usage\":{\"cost\":0.001,\"prompt_tokens\":20,\"completion_tokens\":5,\"prompt_tokens_details\":{\"cached_tokens\":10}}}\n\ndata: [DONE]\n\n")
	})
	b := providers.NewBudget(providers.BudgetLimits{USD: .1})
	r, e := c.Complete(providers.WithBudget(context.Background(), b), providers.Request{PDFs: []providers.PDF{{Name: "a.pdf", Data: []byte("%PDF"), Pages: 1}}, ReasoningEffort: "medium"})
	if e != nil || calls != 2 || r.Usage.Attempts != 2 || r.Usage.FailedAttempts != 1 || r.Usage.CostUSD != .003 || r.Usage.PromptTokens != 30 || r.Usage.CompletionTokens != 8 || r.Usage.ReasoningTokens == nil || *r.Usage.ReasoningTokens != 2 || r.Usage.CachedTokens == nil || *r.Usage.CachedTokens != 10 || b.Snapshot().ReportedUSD != .003 {
		t.Fatalf("%+v %v budget %+v", r, e, b.Snapshot())
	}
}

func TestRetryWithoutUsageRetainsReservationAndMarksUnknownUsage(t *testing.T) {
	calls := 0
	c := clientFor(t, func(w http.ResponseWriter, _ *http.Request) {
		calls++
		if calls == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}],"usage":{"cost":0.001}}`)
	})
	req := providers.Request{Source: "whole source", NoFallback: true, ReasoningEffort: "medium"}
	b := providers.NewBudget(providers.BudgetLimits{USD: .03})
	r, e := c.Complete(providers.WithBudget(context.Background(), b), req)
	if e != nil || r.Usage.UnreportedCalls != 1 || r.Usage.Attempts != 2 || r.Usage.FailedAttempts != 1 || math.Abs(b.Snapshot().CommittedUSD-(c.costBound(req)+.001)) > 1e-12 {
		t.Fatalf("%+v %v budget %+v", r, e, b.Snapshot())
	}
}
