package openrouter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
						Type string `json:"type"`
					} `json:"response_format"`
					Reasoning map[string]any `json:"reasoning"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					t.Fatal(err)
				}
				if body.Model != DefaultModel || body.MaxTokens != 8000 || body.ResponseFormat.Type != "json_schema" || len(body.Messages) != 2 {
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
	for _, key := range []string{"OPENROUTER_KEY", "OPENROUTER_MODEL", "OPENROUTER_BASE_URL", "OPENROUTER_MAX_TOKENS"} {
		t.Setenv(key, "")
	}
	if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "OPENROUTER_KEY") {
		t.Fatalf("err=%v", err)
	}
	t.Setenv("OPENROUTER_KEY", " secret ")
	cfg, err := ConfigFromEnv()
	if err != nil || cfg.APIKey != "secret" || cfg.Model != DefaultModel || cfg.BaseURL != DefaultBaseURL || cfg.MaxTokens != DefaultMaxTokens {
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
