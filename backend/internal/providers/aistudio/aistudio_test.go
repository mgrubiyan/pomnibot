package aistudio

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// TestMain silences the provider's warnings: tests trigger retries on purpose.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

type reply struct {
	status int
	body   string
}

func answer(content, finish string) reply {
	b, _ := json.Marshal(map[string]any{
		"model": "gpt://qwen3.6-35b-a3b/latest",
		"choices": []map[string]any{{
			"message":       map[string]any{"role": "assistant", "content": content},
			"finish_reason": finish,
		}},
		"usage": map[string]int{"prompt_tokens": 900, "completion_tokens": 120},
	})
	return reply{status: http.StatusOK, body: string(b)}
}

func ok(content string) reply { return answer(content, "stop") }

func fail(status int) reply {
	return reply{status: status, body: fmt.Sprintf(`{"error":{"message":"error","code":%d}}`, status)}
}

type seenRequest struct {
	header http.Header
	path   string
	raw    map[string]any
}

// fakeAPI plays the chat completions endpoint.
type fakeAPI struct {
	reply func(n int, raw map[string]any) reply

	mu   sync.Mutex
	reqs []seenRequest
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	var raw map[string]any
	_ = json.NewDecoder(r.Body).Decode(&raw)
	f.mu.Lock()
	f.reqs = append(f.reqs, seenRequest{header: r.Header.Clone(), path: r.URL.Path, raw: raw})
	n := len(f.reqs)
	f.mu.Unlock()
	rep := f.reply(n, raw)
	w.WriteHeader(rep.status)
	_, _ = io.WriteString(w, rep.body)
}

func (f *fakeAPI) requests() []seenRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]seenRequest(nil), f.reqs...)
}

func newTestClient(t *testing.T, api *fakeAPI) *Client {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	c, err := New(Config{
		APIKey:     "api-key",
		FolderID:   "b1gfolder",
		Model:      "qwen3.6-35b-a3b",
		BaseURL:    srv.URL,
		MaxTokens:  4000,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	c.backoff = time.Millisecond
	return c
}

func photoRequest() providers.Request {
	return providers.Request{
		System: "Перепиши страницу.",
		User:   "Перепиши текст этой страницы конспекта.",
		Image:  &providers.Image{Data: []byte("jpeg bytes"), MimeType: "image/jpeg"},
	}
}

// The photo goes as a data URL next to the user's text, and reasoning is off:
// rewriting a page needs no thinking, only tokens and time.
func TestCompleteSendsImage(t *testing.T) {
	api := &fakeAPI{reply: func(int, map[string]any) reply { return ok("Митоз — непрямое деление.") }}
	c := newTestClient(t, api)

	resp, err := c.Complete(context.Background(), photoRequest())
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if string(resp.Content) != "Митоз — непрямое деление." || resp.Model != "qwen3.6-35b-a3b" ||
		resp.Usage.PromptTokens != 900 || resp.Usage.CompletionTokens != 120 {
		t.Errorf("response = %+v", resp)
	}

	r := api.requests()[0]
	if r.path != "/v1/chat/completions" {
		t.Errorf("path = %q", r.path)
	}
	if got := r.header.Get("Authorization"); got != "Api-Key api-key" {
		t.Errorf("Authorization = %q", got)
	}
	if got := r.header.Get("x-folder-id"); got != "b1gfolder" {
		t.Errorf("x-folder-id = %q", got)
	}
	if r.raw["model"] != "gpt://b1gfolder/qwen3.6-35b-a3b" || r.raw["reasoning_effort"] != "none" ||
		r.raw["max_tokens"] != float64(4000) || r.raw["stream"] != false {
		t.Errorf("request = %v", r.raw)
	}

	msgs := r.raw["messages"].([]any)
	system := msgs[0].(map[string]any)
	if system["role"] != "system" || system["content"] != "Перепиши страницу." {
		t.Errorf("system message = %v", system)
	}
	user := msgs[1].(map[string]any)
	parts, _ := user["content"].([]any)
	if user["role"] != "user" || len(parts) != 2 {
		t.Fatalf("user message = %v, want text and image parts", user)
	}
	text, image := parts[0].(map[string]any), parts[1].(map[string]any)
	if text["type"] != "text" || text["text"] != "Перепиши текст этой страницы конспекта." {
		t.Errorf("text part = %v", text)
	}
	wantURL := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString([]byte("jpeg bytes"))
	if image["type"] != "image_url" || image["image_url"].(map[string]any)["url"] != wantURL {
		t.Errorf("image part = %v", image)
	}
}

func TestCompleteSendsPlainTextAndSchema(t *testing.T) {
	api := &fakeAPI{reply: func(int, map[string]any) reply { return ok(`{"cards":[]}`) }}
	c := newTestClient(t, api)

	schema := json.RawMessage(`{"type":"object"}`)
	if _, err := c.Complete(context.Background(), providers.Request{System: "Правила.", User: "Фрагмент.", Schema: schema, Temperature: 0.3}); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	r := api.requests()[0].raw
	user := r["messages"].([]any)[1].(map[string]any)
	if user["content"] != "Фрагмент." {
		t.Errorf("user content = %v, want plain text without an image", user["content"])
	}
	format, _ := r["response_format"].(map[string]any)
	js, _ := format["json_schema"].(map[string]any)
	if format["type"] != "json_schema" || js == nil || js["schema"].(map[string]any)["type"] != "object" {
		t.Errorf("response_format = %v", r["response_format"])
	}
	if r["temperature"] != 0.3 {
		t.Errorf("temperature = %v", r["temperature"])
	}
}

func TestDropsStructuredOutputWhenRejected(t *testing.T) {
	api := &fakeAPI{reply: func(_ int, raw map[string]any) reply {
		if raw["response_format"] != nil {
			return fail(http.StatusBadRequest)
		}
		return ok(`{"cards":[]}`)
	}}
	c := newTestClient(t, api)

	req := providers.Request{System: "Правила.", User: "Фрагмент.", Schema: json.RawMessage(`{"type":"object"}`)}
	for range 2 {
		if _, err := c.Complete(context.Background(), req); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
	}
	reqs := api.requests()
	if len(reqs) != 3 || reqs[1].raw["response_format"] != nil || reqs[2].raw["response_format"] != nil {
		t.Fatalf("%d requests; want rejected, plain, plain", len(reqs))
	}
	if system := reqs[1].raw["messages"].([]any)[0].(map[string]any)["content"].(string); !strings.Contains(system, `{"type":"object"}`) {
		t.Error("without structured output the schema must go into the system prompt")
	}
}

func TestContentFilterIsRefused(t *testing.T) {
	api := &fakeAPI{reply: func(int, map[string]any) reply { return answer("", "content_filter") }}
	c := newTestClient(t, api)

	if _, err := c.Complete(context.Background(), photoRequest()); !errors.Is(err, providers.ErrRefused) {
		t.Errorf("Complete() error = %v, want providers.ErrRefused", err)
	}
}

// An answer cut at the token limit is passed on, marked: the caller asks
// again rather than read a half of a JSON object.
func TestCutAnswerIsTruncated(t *testing.T) {
	api := &fakeAPI{reply: func(int, map[string]any) reply { return answer(`{"facts": [{"cards": [`, "length") }}
	c := newTestClient(t, api)

	resp, err := c.Complete(context.Background(), photoRequest())
	if err != nil || !resp.Truncated {
		t.Errorf("Complete() = %+v, %v; want a truncated answer", resp, err)
	}
	api.reply = func(int, map[string]any) reply { return ok("текст") }
	if resp, _ := c.Complete(context.Background(), photoRequest()); resp.Truncated {
		t.Errorf("a whole answer marked truncated")
	}
}

func TestRetriesServerErrors(t *testing.T) {
	api := &fakeAPI{reply: func(n int, _ map[string]any) reply {
		if n < 3 {
			return fail(http.StatusServiceUnavailable)
		}
		return ok("текст")
	}}
	c := newTestClient(t, api)

	if _, err := c.Complete(context.Background(), photoRequest()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if n := len(api.requests()); n != 3 {
		t.Errorf("%d requests, want 3", n)
	}
}

func TestReportsUnavailable(t *testing.T) {
	for _, status := range []int{http.StatusServiceUnavailable, http.StatusTooManyRequests, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			c := newTestClient(t, &fakeAPI{reply: func(int, map[string]any) reply { return fail(status) }})
			if _, err := c.Complete(context.Background(), photoRequest()); !errors.Is(err, providers.ErrUnavailable) {
				t.Errorf("Complete() error = %v, want providers.ErrUnavailable", err)
			}
		})
	}
}

func TestRequestErrorsAreNotUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply reply
	}{
		{"bad request", fail(http.StatusBadRequest)},
		{"undecodable answer", reply{status: http.StatusOK, body: "<html>proxy error</html>"}},
		{"no choices", reply{status: http.StatusOK, body: `{"choices":[]}`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := newTestClient(t, &fakeAPI{reply: func(int, map[string]any) reply { return tc.reply }})
			_, err := c.Complete(context.Background(), photoRequest())
			if err == nil || errors.Is(err, providers.ErrUnavailable) {
				t.Errorf("Complete() error = %v, want an error that is not providers.ErrUnavailable", err)
			}
		})
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("YC_API_KEY", "api-key")
	t.Setenv("YC_FOLDER_ID", "b1gfolder")
	t.Setenv("YC_LLM_MODEL", "")
	t.Setenv("YC_LLM_BASE_URL", "")
	t.Setenv("YC_LLM_MAX_TOKENS", "")
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v", err)
	}
	if cfg.Model != "qwen3.6-35b-a3b" || cfg.BaseURL != DefaultBaseURL || cfg.MaxTokens != 8000 {
		t.Errorf("config = %+v, want Qwen, the default URL and 8000 tokens: a fragment's cards take that", cfg)
	}

	t.Setenv("YC_LLM_MODEL", "gemma-3-27b-it")
	t.Setenv("YC_LLM_MAX_TOKENS", "4000")
	if cfg, err = ConfigFromEnv(); err != nil || cfg.Model != "gemma-3-27b-it" || cfg.MaxTokens != 4000 {
		t.Errorf("ConfigFromEnv() = %+v, %v; want the given model and token cap", cfg, err)
	}
}

func TestConfigErrors(t *testing.T) {
	for _, tc := range []struct{ key, val string }{
		{"YC_API_KEY", ""},
		{"YC_FOLDER_ID", ""},
		{"YC_LLM_BASE_URL", "llm.test"},
		{"YC_LLM_MAX_TOKENS", "many"},
		{"YC_LLM_MAX_TOKENS", "0"},
	} {
		t.Run(tc.key+"="+tc.val, func(t *testing.T) {
			t.Setenv("YC_API_KEY", "api-key")
			t.Setenv("YC_FOLDER_ID", "b1gfolder")
			t.Setenv("YC_LLM_BASE_URL", "")
			t.Setenv("YC_LLM_MAX_TOKENS", "")
			t.Setenv(tc.key, tc.val)
			if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Errorf("ConfigFromEnv() error = %v, want one naming %s", err, tc.key)
			}
		})
	}
}
