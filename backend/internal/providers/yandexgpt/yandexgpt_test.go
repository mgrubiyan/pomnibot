package yandexgpt

import (
	"context"
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

// answer is a completion as the REST API returns it: wrapped in "result",
// token counts as strings.
func answer(text, status string) reply {
	b, _ := json.Marshal(map[string]any{"result": map[string]any{
		"alternatives": []map[string]any{{
			"message": map[string]string{"role": "assistant", "text": text},
			"status":  status,
		}},
		"usage":        map[string]string{"inputTextTokens": "120", "completionTokens": "30", "totalTokens": "150"},
		"modelVersion": "23.10.2026",
	}})
	return reply{status: http.StatusOK, body: string(b)}
}

func ok(text string) reply { return answer(text, "ALTERNATIVE_STATUS_FINAL") }

func fail(status int) reply {
	return reply{status: status, body: fmt.Sprintf(`{"error":{"grpcCode":3,"httpCode":%d,"message":"error"}}`, status)}
}

type seenRequest struct {
	header http.Header
	path   string
	body   completionRequest
	raw    map[string]any
}

// fakeAPI plays the completion endpoint.
type fakeAPI struct {
	reply func(n int, req completionRequest) reply

	mu   sync.Mutex
	reqs []seenRequest
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	data, _ := io.ReadAll(r.Body)
	var req completionRequest
	_ = json.Unmarshal(data, &req)
	var raw map[string]any
	_ = json.Unmarshal(data, &raw)
	f.mu.Lock()
	f.reqs = append(f.reqs, seenRequest{header: r.Header.Clone(), path: r.URL.Path, body: req, raw: raw})
	n := len(f.reqs)
	f.mu.Unlock()
	rep := f.reply(n, req)
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
		Model:      "yandexgpt-5.1",
		BaseURL:    srv.URL,
		MaxTokens:  8000,
		HTTPClient: srv.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	c.backoff = time.Millisecond
	return c
}

var schema = json.RawMessage(`{"type":"object","properties":{"cards":{"type":"array"}},"required":["cards"]}`)

func request() providers.Request {
	return providers.Request{System: "Правила.", User: "Фрагмент.", Schema: schema, Temperature: 0.2}
}

func TestCompleteSendsStructuredOutput(t *testing.T) {
	api := &fakeAPI{reply: func(int, completionRequest) reply { return ok(`{"cards":[]}`) }}
	c := newTestClient(t, api)

	resp, err := c.Complete(context.Background(), request())
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if string(resp.Content) != `{"cards":[]}` || resp.Model != "yandexgpt-5.1:23.10.2026" ||
		resp.Usage.PromptTokens != 120 || resp.Usage.CompletionTokens != 30 {
		t.Errorf("response = %+v", resp)
	}

	reqs := api.requests()
	if len(reqs) != 1 {
		t.Fatalf("%d requests, want 1", len(reqs))
	}
	r := reqs[0]
	if r.path != "/foundationModels/v1/completion" {
		t.Errorf("path = %q", r.path)
	}
	if got := r.header.Get("Authorization"); got != "Api-Key api-key" {
		t.Errorf("Authorization = %q", got)
	}
	if got := r.header.Get("x-folder-id"); got != "b1gfolder" {
		t.Errorf("x-folder-id = %q", got)
	}
	if r.body.ModelURI != "gpt://b1gfolder/yandexgpt-5.1" {
		t.Errorf("modelUri = %q", r.body.ModelURI)
	}
	opts := r.raw["completionOptions"].(map[string]any)
	if opts["stream"] != false || opts["temperature"] != 0.2 || opts["maxTokens"] != "8000" {
		t.Errorf("completionOptions = %v, want no stream, the temperature, maxTokens as a string", opts)
	}
	msgs := r.body.Messages
	if len(msgs) != 2 || msgs[0].Role != "system" || msgs[0].Text != "Правила." ||
		msgs[1].Role != "user" || msgs[1].Text != "Фрагмент." {
		t.Errorf("messages = %+v", msgs)
	}
	if r.body.JSONSchema == nil || string(r.body.JSONSchema.Schema) != string(schema) {
		t.Errorf("jsonSchema = %+v", r.body.JSONSchema)
	}
}

// The gRPC reference shows the completion unwrapped; take it either way.
func TestParsesUnwrappedResponse(t *testing.T) {
	api := &fakeAPI{reply: func(int, completionRequest) reply {
		return reply{status: http.StatusOK, body: `{"alternatives":[{"message":{"role":"assistant","text":"{}"},"status":"ALTERNATIVE_STATUS_FINAL"}],"usage":{"inputTextTokens":"5","completionTokens":"1"},"modelVersion":"v"}`}
	}}
	c := newTestClient(t, api)

	resp, err := c.Complete(context.Background(), request())
	if err != nil || string(resp.Content) != "{}" || resp.Usage.PromptTokens != 5 {
		t.Errorf("Complete() = %+v, %v", resp, err)
	}
}

func TestModelMayBeAFullURI(t *testing.T) {
	api := &fakeAPI{reply: func(int, completionRequest) reply { return ok("{}") }}
	c := newTestClient(t, api)
	c.cfg.Model = "gpt://b1gother/yandexgpt-5-pro"

	if _, err := c.Complete(context.Background(), request()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if got := api.requests()[0].body.ModelURI; got != "gpt://b1gother/yandexgpt-5-pro" {
		t.Errorf("modelUri = %q, want the URI as given", got)
	}
}

// The API wants 0 ≤ temperature ≤ 1.
func TestTemperatureIsClamped(t *testing.T) {
	api := &fakeAPI{reply: func(int, completionRequest) reply { return ok("{}") }}
	c := newTestClient(t, api)

	req := request()
	req.Temperature = 1.5
	if _, err := c.Complete(context.Background(), req); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if got := api.requests()[0].body.CompletionOptions.Temperature; got != 1 {
		t.Errorf("temperature = %v, want 1", got)
	}
}

func TestContentFilterIsRefused(t *testing.T) {
	api := &fakeAPI{reply: func(int, completionRequest) reply {
		return answer("Я не могу обсуждать эту тему.", "ALTERNATIVE_STATUS_CONTENT_FILTER")
	}}
	c := newTestClient(t, api)

	_, err := c.Complete(context.Background(), request())
	if !errors.Is(err, providers.ErrRefused) {
		t.Errorf("Complete() error = %v, want providers.ErrRefused", err)
	}
	if len(api.requests()) != 1 {
		t.Errorf("%d requests, want 1: a filter does not pass with retries", len(api.requests()))
	}
}

func TestRetriesRateLimitAndServerErrors(t *testing.T) {
	api := &fakeAPI{reply: func(n int, _ completionRequest) reply {
		switch n {
		case 1:
			return fail(http.StatusTooManyRequests)
		case 2:
			return fail(http.StatusServiceUnavailable)
		}
		return ok(`{"cards":[]}`)
	}}
	c := newTestClient(t, api)

	if _, err := c.Complete(context.Background(), request()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if n := len(api.requests()); n != 3 {
		t.Errorf("%d requests, want 3", n)
	}
}

// Structured output may not take every schema. If the API rejects it, the
// schema moves into the system prompt, and later calls skip straight to that.
func TestDropsStructuredOutputWhenRejected(t *testing.T) {
	api := &fakeAPI{reply: func(_ int, req completionRequest) reply {
		if req.JSONSchema != nil {
			return fail(http.StatusBadRequest)
		}
		return ok("```json\n{\"cards\":[]}\n```")
	}}
	c := newTestClient(t, api)

	for range 2 {
		resp, err := c.Complete(context.Background(), request())
		if err != nil || !strings.Contains(string(resp.Content), `{"cards":[]}`) {
			t.Fatalf("Complete() = %q, %v", resp.Content, err)
		}
	}
	reqs := api.requests()
	if len(reqs) != 3 {
		t.Fatalf("%d requests, want 3: rejected, plain, plain", len(reqs))
	}
	if reqs[0].body.JSONSchema == nil || reqs[1].body.JSONSchema != nil || reqs[2].body.JSONSchema != nil {
		t.Error("structured output was not dropped after the rejection, or was retried again")
	}
	if !strings.Contains(reqs[1].body.Messages[0].Text, string(schema)) {
		t.Error("without structured output the schema must go into the system prompt")
	}
}

// YandexGPT reads text only. A photo fails at once, with no request, so the
// caller moves on to its OCR without waiting.
func TestImageIsNotSupported(t *testing.T) {
	api := &fakeAPI{reply: func(int, completionRequest) reply { return ok("{}") }}
	c := newTestClient(t, api)

	req := request()
	req.Image = &providers.Image{Data: []byte("jpeg"), MimeType: "image/jpeg"}
	_, err := c.Complete(context.Background(), req)
	if err == nil || errors.Is(err, providers.ErrUnavailable) {
		t.Errorf("Complete() error = %v, want an error that is not providers.ErrUnavailable", err)
	}
	if n := len(api.requests()); n != 0 {
		t.Errorf("%d requests, want none", n)
	}
}

func TestReportsUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		closed bool // the host refuses connections
	}{
		{name: "server errors", status: http.StatusServiceUnavailable},
		{name: "rate limited after retries", status: http.StatusTooManyRequests},
		{name: "key rejected", status: http.StatusUnauthorized},
		{name: "no access to the model", status: http.StatusForbidden},
		{name: "unknown model", status: http.StatusNotFound},
		{name: "host unreachable", closed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAPI{reply: func(int, completionRequest) reply { return fail(tc.status) }}
			c := newTestClient(t, api)
			if tc.closed {
				down := httptest.NewServer(http.NotFoundHandler())
				c.cfg.BaseURL = down.URL
				down.Close()
			}

			_, err := c.Complete(context.Background(), request())
			if !errors.Is(err, providers.ErrUnavailable) {
				t.Errorf("Complete() error = %v, want providers.ErrUnavailable", err)
			}
		})
	}
}

func TestRequestErrorsAreNotUnavailable(t *testing.T) {
	for _, tc := range []struct {
		name  string
		reply func(int, completionRequest) reply
	}{
		{name: "bad request", reply: func(int, completionRequest) reply { return fail(http.StatusBadRequest) }},
		{name: "undecodable answer", reply: func(int, completionRequest) reply {
			return reply{status: http.StatusOK, body: "<html>proxy error</html>"}
		}},
		{name: "no alternatives", reply: func(int, completionRequest) reply {
			return reply{status: http.StatusOK, body: `{"result":{"alternatives":[]}}`}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			api := &fakeAPI{reply: tc.reply}
			c := newTestClient(t, api)
			_, err := c.Complete(context.Background(), request())
			if err == nil || errors.Is(err, providers.ErrUnavailable) {
				t.Errorf("Complete() error = %v, want an error that is not providers.ErrUnavailable", err)
			}
		})
	}
}

func TestCallerCancelIsNotUnavailable(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	api := &fakeAPI{reply: func(int, completionRequest) reply {
		cancel()
		return fail(http.StatusServiceUnavailable)
	}}
	c := newTestClient(t, api)

	_, err := c.Complete(ctx, request())
	if !errors.Is(err, context.Canceled) || errors.Is(err, providers.ErrUnavailable) {
		t.Errorf("Complete() error = %v, want context.Canceled only", err)
	}
}

func setYandexEnv(t *testing.T) {
	t.Helper()
	t.Setenv("YC_API_KEY", "api-key")
	t.Setenv("YC_FOLDER_ID", "b1gfolder")
	t.Setenv("YC_LLM_MODEL", "yandexgpt-5.1")
	t.Setenv("YC_LLM_BASE_URL", "https://llm.test/")
	t.Setenv("YC_LLM_MAX_TOKENS", "4000")
}

func TestConfigFromEnv(t *testing.T) {
	setYandexEnv(t)
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v", err)
	}
	want := Config{APIKey: "api-key", FolderID: "b1gfolder", Model: "yandexgpt-5.1", BaseURL: "https://llm.test", MaxTokens: 4000}
	if cfg.APIKey != want.APIKey || cfg.FolderID != want.FolderID || cfg.Model != want.Model ||
		cfg.BaseURL != want.BaseURL || cfg.MaxTokens != want.MaxTokens {
		t.Errorf("config = %+v, want %+v", cfg, want)
	}
}

func TestConfigDefaults(t *testing.T) {
	setYandexEnv(t)
	t.Setenv("YC_LLM_MODEL", "")
	t.Setenv("YC_LLM_BASE_URL", "")
	t.Setenv("YC_LLM_MAX_TOKENS", "")
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v", err)
	}
	if cfg.Model != DefaultModel || cfg.BaseURL != DefaultBaseURL || cfg.MaxTokens != DefaultMaxTokens {
		t.Errorf("config = %+v, want the default model, URL and token cap", cfg)
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
			setYandexEnv(t)
			t.Setenv(tc.key, tc.val)
			_, err := ConfigFromEnv()
			if err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Errorf("ConfigFromEnv() error = %v, want one naming %s", err, tc.key)
			}
		})
	}
}
