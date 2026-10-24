package app

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/config"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/aistudio"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/gigachat"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
)

type thinkingTransport func(*http.Request) (*http.Response, error)

func (f thinkingTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestChosenThinkingModeSurvivesMainFlowTimeoutsAndBudget(t *testing.T) {
	for _, key := range []string{"OPENROUTER_MODEL", "OPENROUTER_BASE_URL", "OPENROUTER_MAX_TOKENS", "OPENROUTER_EFFORT", "LLM_PRIMARY_TIMEOUT"} {
		t.Setenv(key, "")
	}
	t.Setenv("OPENROUTER_KEY", "test")
	router, err := openrouter.ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	called := false
	router.HTTPClient = &http.Client{Transport: thinkingTransport(func(r *http.Request) (*http.Response, error) {
		called = true
		deadline, ok := r.Context().Deadline()
		if !ok || time.Until(deadline) < 4*time.Minute {
			t.Error("main fallback would interrupt high thinking")
		}
		var body struct {
			MaxTokens int `json:"max_tokens"`
			Reasoning struct {
				Enabled bool   `json:"enabled"`
				Effort  string `json:"effort"`
			} `json:"reasoning"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body.MaxTokens != 50000 || !body.Reasoning.Enabled || body.Reasoning.Effort != "high" {
			t.Errorf("chosen mode lost: %+v", body)
		}
		return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(`{"choices":[{"message":{"content":"{}"},"finish_reason":"stop"}],"usage":{"cost":0.0256031}}`))}, nil
	})}
	cfg := config.Config{
		OpenRouter: &router,
		AIStudio:   &aistudio.Config{APIKey: "key", FolderID: "folder", Model: "qwen", BaseURL: "https://example.com", MaxTokens: 8000},
		GigaChat:   gigachat.Config{ClientID: "client", ClientSecret: "secret", Scope: "scope", Model: "giga", BaseURL: "https://example.com", AuthURL: "https://example.com/oauth", MaxTokens: 8000},
		LLM:        config.LLMConfig{Primary: config.PrimaryOpenRouter, PrimaryTimeout: 5 * time.Minute, Cooldown: time.Minute},
	}
	m, err := newModels(cfg)
	if err != nil {
		t.Fatal(err)
	}
	budget := providers.NewBudget(providers.BudgetLimits{USD: .15})
	finish, err := providers.ReserveCall(providers.WithBudget(context.Background(), budget), .0191651)
	if err != nil {
		t.Fatal(err)
	}
	finish(providers.Usage{CostUSD: .0191651}, true)
	ctx, cancel := context.WithTimeout(providers.WithBudget(context.Background(), budget), generator.DefaultCallTimeout)
	defer cancel()
	_, err = m.text.Complete(ctx, providers.Request{User: strings.Repeat("x", 80000)})
	if err != nil || !called {
		t.Fatalf("main flow failed before request: called=%v err=%v", called, err)
	}
	router.Effort = "medium"
	other, err := newModels(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if m.identity == other.identity || m.readingIdentity == other.readingIdentity {
		t.Fatal("changed thinking effort reused old model cache identity")
	}
}

func TestOpenRouterFallbackOrderAndSharedCooldown(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/router/chat/completions":
			calls = append(calls, "openrouter")
			w.WriteHeader(402)
		case "/oauth":
			calls = append(calls, "gigachat")
			w.WriteHeader(401)
		default:
			calls = append(calls, "qwen")
			_, _ = fmt.Fprint(w, `{"choices":[{"message":{"content":"ok"}}]}`)
		}
	}))
	defer srv.Close()
	cfg := config.Config{
		OpenRouter: &openrouter.Config{APIKey: "key", Model: openrouter.DefaultModel, BaseURL: srv.URL + "/router", MaxTokens: 8000, HTTPClient: srv.Client()},
		AIStudio:   &aistudio.Config{APIKey: "key", FolderID: "folder", Model: "qwen", BaseURL: srv.URL, MaxTokens: 8000, HTTPClient: srv.Client()},
		GigaChat:   gigachat.Config{ClientID: "client", ClientSecret: "secret", Scope: "scope", Model: "giga", BaseURL: srv.URL, AuthURL: srv.URL + "/oauth", MaxTokens: 8000, HTTPClient: srv.Client()},
		LLM:        config.LLMConfig{Primary: config.PrimaryOpenRouter, PrimaryTimeout: time.Second, Cooldown: time.Minute},
	}
	llm, err := newModels(cfg)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []providers.Provider{llm.text, llm.vision} {
		res, err := p.Complete(context.Background(), providers.Request{User: "notes"})
		if err != nil || string(res.Content) != "ok" {
			t.Fatalf("res=%+v err=%v", res, err)
		}
	}
	if !reflect.DeepEqual(calls, []string{"openrouter", "gigachat", "qwen", "qwen"}) {
		t.Fatalf("order/cooldown = %v", calls)
	}
}
