package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/config"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/aistudio"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/gigachat"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
)

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
