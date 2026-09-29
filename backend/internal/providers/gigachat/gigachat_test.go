package gigachat

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
	"regexp"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// TestMain silences the provider's warnings: tests trigger retries and
// fallbacks on purpose.
func TestMain(m *testing.M) {
	slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
	os.Exit(m.Run())
}

type chatReply struct {
	status int
	body   string
	header map[string]string
}

func ok(content string) chatReply {
	b, _ := json.Marshal(map[string]any{
		"model": "GigaChat-3-Ultra:3.0.1",
		"choices": []map[string]any{{
			"message":       map[string]string{"role": "assistant", "content": content},
			"finish_reason": "stop",
		}},
		"usage": map[string]int{"prompt_tokens": 120, "completion_tokens": 30, "total_tokens": 150},
	})
	return chatReply{status: http.StatusOK, body: string(b)}
}

func fail(status int) chatReply {
	return chatReply{status: status, body: fmt.Sprintf(`{"status":%d,"message":"error"}`, status)}
}

// fakeAPI plays both the OAuth endpoint and chat completions.
type fakeAPI struct {
	tokenTTL    time.Duration
	oauthStatus atomic.Int32 // non-zero: the OAuth endpoint fails with it
	chat        func(n int, req chatRequest) chatReply

	oauthCalls atomic.Int32
	mu         sync.Mutex
	oauthReqs  []*http.Request
	oauthForms []string
	chatReqs   []chatRequest
	chatAuth   []string
	uploads    []upload // files sent to /files
	deleted    []string // ids deleted
}

type upload struct {
	purpose, filename, mime string
	data                    []byte
}

func (f *fakeAPI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/oauth":
		n := f.oauthCalls.Add(1)
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.oauthReqs = append(f.oauthReqs, r)
		f.oauthForms = append(f.oauthForms, string(body))
		f.mu.Unlock()
		if status := int(f.oauthStatus.Load()); status != 0 {
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"code":7,"message":"scope is invalid"}`)
			return
		}
		ttl := f.tokenTTL
		if ttl == 0 {
			ttl = 30 * time.Minute
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": fmt.Sprintf("token-%d", n),
			"expires_at":   time.Now().Add(ttl).UnixMilli(),
		})
	case "/v1/chat/completions":
		var req chatRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		f.mu.Lock()
		f.chatReqs = append(f.chatReqs, req)
		f.chatAuth = append(f.chatAuth, r.Header.Get("Authorization"))
		n := len(f.chatReqs)
		f.mu.Unlock()
		reply := f.chat(n, req)
		for k, v := range reply.header {
			w.Header().Set(k, v)
		}
		w.WriteHeader(reply.status)
		_, _ = io.WriteString(w, reply.body)
	case "/v1/files":
		file, header, err := r.FormFile("file")
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		data, _ := io.ReadAll(file)
		f.mu.Lock()
		f.uploads = append(f.uploads, upload{
			purpose: r.FormValue("purpose"), filename: header.Filename,
			mime: header.Header.Get("Content-Type"), data: data,
		})
		id := fmt.Sprintf("file-%d", len(f.uploads))
		f.mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "object": "file", "purpose": "general"})
	default:
		if id, ok := strings.CutSuffix(strings.TrimPrefix(r.URL.Path, "/v1/files/"), "/delete"); ok && r.Method == http.MethodPost {
			f.mu.Lock()
			f.deleted = append(f.deleted, id)
			f.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"id": id, "deleted": true})
			return
		}
		http.NotFound(w, r)
	}
}

func (f *fakeAPI) requests() ([]chatRequest, []string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]chatRequest(nil), f.chatReqs...), append([]string(nil), f.chatAuth...)
}

func newTestClient(t *testing.T, api *fakeAPI) *Client {
	t.Helper()
	srv := httptest.NewServer(api)
	t.Cleanup(srv.Close)
	c, err := New(Config{
		ClientID:      "client-id",
		ClientSecret:  "client-secret",
		Model:         "GigaChat-3-Ultra",
		FallbackModel: "GigaChat-2-Max",
		BaseURL:       srv.URL + "/v1",
		AuthURL:       srv.URL + "/oauth",
		HTTPClient:    srv.Client(),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	c.backoff = time.Millisecond
	t.Cleanup(c.Close)
	return c
}

var schema = json.RawMessage(`{"type":"object","properties":{"cards":{"type":"array"}},"required":["cards"]}`)

func request() providers.Request {
	return providers.Request{System: "Правила.", User: "Фрагмент.", Schema: schema}
}

func TestCompleteSendsStructuredOutput(t *testing.T) {
	api := &fakeAPI{chat: func(int, chatRequest) chatReply { return ok(`{"cards":[]}`) }}
	c := newTestClient(t, api)

	resp, err := c.Complete(context.Background(), request())
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if string(resp.Content) != `{"cards":[]}` || resp.Model != "GigaChat-3-Ultra:3.0.1" ||
		resp.Usage.PromptTokens != 120 || resp.Usage.CompletionTokens != 30 {
		t.Errorf("response = %+v", resp)
	}

	reqs, auth := api.requests()
	req := reqs[0]
	if req.Model != "GigaChat-3-Ultra" || req.Stream || req.MaxTokens != DefaultMaxTokens {
		t.Errorf("request = %+v", req)
	}
	if req.Temperature <= 0 || req.Temperature > 0.001 {
		t.Errorf("temperature = %v, want the deterministic mode", req.Temperature)
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" || req.Messages[0].Content != "Правила." ||
		req.Messages[1].Role != "user" || req.Messages[1].Content != "Фрагмент." {
		t.Errorf("messages = %+v", req.Messages)
	}
	if rf := req.ResponseFormat; rf == nil || rf.Type != "json_schema" || !rf.Strict || string(rf.Schema) != string(schema) {
		t.Errorf("response_format = %+v", rf)
	}
	if auth[0] != "Bearer token-1" {
		t.Errorf("Authorization = %q", auth[0])
	}

	oauth := api.oauthReqs[0]
	wantBasic := "Basic " + base64.StdEncoding.EncodeToString([]byte("client-id:client-secret"))
	if oauth.Header.Get("Authorization") != wantBasic {
		t.Errorf("OAuth Authorization = %q", oauth.Header.Get("Authorization"))
	}
	uuid4 := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	if !uuid4.MatchString(oauth.Header.Get("RqUID")) {
		t.Errorf("RqUID = %q, want a UUID v4", oauth.Header.Get("RqUID"))
	}
	if api.oauthForms[0] != "scope=GIGACHAT_API_PERS" {
		t.Errorf("OAuth form = %q", api.oauthForms[0])
	}
}

func TestTokenIsSharedAndRefreshedAhead(t *testing.T) {
	api := &fakeAPI{
		tokenTTL: 2 * time.Second,
		chat:     func(int, chatRequest) chatReply { return ok(`{}`) },
	}
	c := newTestClient(t, api)
	c.tokens.minValid = 100 * time.Millisecond
	c.tokens.refreshBefore = 1500 * time.Millisecond // refresh a second after issue

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := c.Complete(context.Background(), request()); err != nil {
				t.Errorf("Complete() error = %v", err)
			}
		})
	}
	wg.Wait()
	if n := api.oauthCalls.Load(); n != 1 {
		t.Fatalf("8 concurrent calls fetched %d tokens, want 1", n)
	}

	// No requests in between: the timer alone must replace the token. Wait
	// for the client to hold it, not for the server to see the request: the
	// answer is parsed and stored a moment later.
	deadline := time.Now().Add(3 * time.Second)
	for {
		if tok, _ := c.tokens.cached(); tok == "token-2" {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("token was not refreshed ahead of expiry")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := c.Complete(context.Background(), request()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	_, auth := api.requests()
	if last := auth[len(auth)-1]; last != "Bearer token-2" {
		t.Errorf("after refresh Authorization = %q, want the new token", last)
	}
}

func TestRetriesRateLimitAndServerErrors(t *testing.T) {
	api := &fakeAPI{chat: func(n int, _ chatRequest) chatReply {
		switch n {
		case 1:
			r := fail(http.StatusTooManyRequests)
			r.header = map[string]string{"Retry-After": "0"}
			return r
		case 2:
			return fail(http.StatusServiceUnavailable)
		}
		return ok(`{"cards":[]}`)
	}}
	c := newTestClient(t, api)

	if _, err := c.Complete(context.Background(), request()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	reqs, _ := api.requests()
	if len(reqs) != 3 {
		t.Errorf("%d chat requests, want 3", len(reqs))
	}
	for _, r := range reqs {
		if r.Model != "GigaChat-3-Ultra" {
			t.Errorf("model = %q, want no fallback for transient errors that pass", r.Model)
		}
	}
}

func TestFallsBackWhenModelIsOutOfQuota(t *testing.T) {
	api := &fakeAPI{chat: func(_ int, req chatRequest) chatReply {
		if req.Model == "GigaChat-3-Ultra" {
			return fail(http.StatusPaymentRequired)
		}
		return ok(`{"cards":[]}`)
	}}
	c := newTestClient(t, api)

	for range 2 {
		if _, err := c.Complete(context.Background(), request()); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
	}
	reqs, _ := api.requests()
	var models []string
	for _, r := range reqs {
		models = append(models, r.Model)
	}
	// The second call goes straight to the fallback: no point asking a model
	// that has just run out of tokens.
	want := []string{"GigaChat-3-Ultra", "GigaChat-2-Max", "GigaChat-2-Max"}
	if strings.Join(models, ",") != strings.Join(want, ",") {
		t.Errorf("models = %v, want %v", models, want)
	}
}

func TestFallsBackWhenModelKeepsFailing(t *testing.T) {
	api := &fakeAPI{chat: func(_ int, req chatRequest) chatReply {
		if req.Model == "GigaChat-3-Ultra" {
			return fail(http.StatusInternalServerError)
		}
		return ok(`{"cards":[]}`)
	}}
	c := newTestClient(t, api)

	if _, err := c.Complete(context.Background(), request()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	reqs, _ := api.requests()
	if len(reqs) != maxAttempts+1 || reqs[len(reqs)-1].Model != "GigaChat-2-Max" {
		t.Errorf("%d requests, last to %q; want %d retries then the fallback", len(reqs), reqs[len(reqs)-1].Model, maxAttempts)
	}
}

// An image goes up to the file storage, the user message refers to it, and
// the file is deleted once the answer is in: students' notes are not kept.
func TestCompleteSendsImage(t *testing.T) {
	api := &fakeAPI{chat: func(int, chatRequest) chatReply { return ok("Текст страницы.") }}
	c := newTestClient(t, api)

	req := providers.Request{System: "Перепиши страницу.", User: "Страница.",
		Image: &providers.Image{Data: []byte("jpeg bytes"), MimeType: "image/jpeg"}}
	resp, err := c.Complete(context.Background(), req)
	if err != nil || string(resp.Content) != "Текст страницы." {
		t.Fatalf("Complete() = %q, %v", resp.Content, err)
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if len(api.uploads) != 1 || string(api.uploads[0].data) != "jpeg bytes" ||
		api.uploads[0].mime != "image/jpeg" || api.uploads[0].purpose != "general" {
		t.Fatalf("uploads = %+v, want the image once, purpose general", api.uploads)
	}
	msgs := api.chatReqs[0].Messages
	if user := msgs[len(msgs)-1]; user.Role != "user" || !slices.Equal(user.Attachments, []string{"file-1"}) {
		t.Errorf("user message = %+v, want the file attached", user)
	}
	if !slices.Equal(api.deleted, []string{"file-1"}) {
		t.Errorf("deleted = %v, want the uploaded file", api.deleted)
	}
}

func TestCompleteDeletesImageWhenModelFails(t *testing.T) {
	api := &fakeAPI{chat: func(int, chatRequest) chatReply { return fail(http.StatusBadRequest) }}
	c := newTestClient(t, api)

	req := providers.Request{User: "Страница.", Image: &providers.Image{Data: []byte("png"), MimeType: "image/png"}}
	if _, err := c.Complete(context.Background(), req); err == nil {
		t.Fatal("Complete() succeeded, want the model's error")
	}
	api.mu.Lock()
	defer api.mu.Unlock()
	if !slices.Equal(api.deleted, []string{"file-1"}) {
		t.Errorf("deleted = %v, want the file deleted despite the error", api.deleted)
	}
}

func blacklisted() chatReply {
	b, _ := json.Marshal(map[string]any{
		"model": "GigaChat-3-Ultra:3.0.1",
		"choices": []map[string]any{{
			"message":       map[string]string{"role": "assistant", "content": "Как и любая языковая модель, GigaChat не обладает собственным мнением."},
			"finish_reason": "blacklist",
		}},
		"usage": map[string]int{"prompt_tokens": 120, "completion_tokens": 20, "total_tokens": 140},
	})
	return chatReply{status: http.StatusOK, body: string(b)}
}

// The content filter of one model blocks history lectures now and then; the
// other model may pass the same text. The filter is about the text, not the
// model, so the next request still goes to the main model first.
func TestFallsBackWhenContentFilterBlocks(t *testing.T) {
	api := &fakeAPI{chat: func(_ int, req chatRequest) chatReply {
		if req.Model == "GigaChat-3-Ultra" {
			return blacklisted()
		}
		return ok(`{"cards":[]}`)
	}}
	c := newTestClient(t, api)

	for range 2 {
		resp, err := c.Complete(context.Background(), request())
		if err != nil || string(resp.Content) != `{"cards":[]}` {
			t.Fatalf("Complete() = %q, %v; want the fallback's answer", resp.Content, err)
		}
	}
	reqs, _ := api.requests()
	var models []string
	for _, r := range reqs {
		models = append(models, r.Model)
	}
	want := []string{"GigaChat-3-Ultra", "GigaChat-2-Max", "GigaChat-3-Ultra", "GigaChat-2-Max"}
	if strings.Join(models, ",") != strings.Join(want, ",") {
		t.Errorf("models = %v, want %v", models, want)
	}
}

func TestReportsContentFilterOfEveryModel(t *testing.T) {
	api := &fakeAPI{chat: func(int, chatRequest) chatReply { return blacklisted() }}
	c := newTestClient(t, api)

	_, err := c.Complete(context.Background(), request())
	if !errors.Is(err, providers.ErrRefused) {
		t.Errorf("Complete() error = %v, want providers.ErrRefused", err)
	}
	if reqs, _ := api.requests(); len(reqs) != 2 {
		t.Errorf("%d requests, want one per model: a filter does not pass with retries", len(reqs))
	}
}

func TestDropsStructuredOutputWhenRejected(t *testing.T) {
	api := &fakeAPI{chat: func(_ int, req chatRequest) chatReply {
		if req.ResponseFormat != nil {
			return fail(http.StatusUnprocessableEntity)
		}
		return ok("```json\n{\"cards\":[]}\n```")
	}}
	c := newTestClient(t, api)

	for range 2 {
		resp, err := c.Complete(context.Background(), request())
		if err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		if !strings.Contains(string(resp.Content), `{"cards":[]}`) {
			t.Errorf("content = %q", resp.Content)
		}
	}
	reqs, _ := api.requests()
	if len(reqs) != 3 {
		t.Fatalf("%d requests, want 3: rejected, plain, plain", len(reqs))
	}
	if reqs[0].ResponseFormat == nil || reqs[1].ResponseFormat != nil || reqs[2].ResponseFormat != nil {
		t.Error("structured output was not dropped after the rejection, or was retried again")
	}
	if !strings.Contains(reqs[1].Messages[0].Content, string(schema)) {
		t.Error("without structured output the schema must go into the system prompt")
	}
}

func TestDoesNotRetryBadCredentials(t *testing.T) {
	api := &fakeAPI{chat: func(int, chatRequest) chatReply { return fail(http.StatusUnauthorized) }}
	c := newTestClient(t, api)

	_, err := c.Complete(context.Background(), request())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("error = %v, want a 401 APIError", err)
	}
	if reqs, _ := api.requests(); len(reqs) != 1 {
		t.Errorf("%d requests, want 1: neither retries nor fallback help a 401", len(reqs))
	}
}

func TestConfig(t *testing.T) {
	t.Setenv("GIGACHAT_AUTH_KEY", "")
	t.Setenv("GIGACHAT_CLIENT_ID", "id")
	t.Setenv("GIGACHAT_CLIENT_SECRET", "secret")
	t.Setenv("GIGACHAT_SCOPE", "")
	t.Setenv("GIGACHAT_MODEL", "")
	t.Setenv("GIGACHAT_FALLBACK_MODEL", "GigaChat-2-Max")
	t.Setenv("GIGACHAT_BASE_URL", "")
	t.Setenv("GIGACHAT_AUTH_URL", "")
	t.Setenv("GIGACHAT_MAX_TOKENS", "")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v", err)
	}
	c, err := New(cfg)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer c.Close()
	if c.cfg.Scope != DefaultScope || c.cfg.Model != DefaultModel || c.cfg.FallbackModel != "GigaChat-2-Max" ||
		c.cfg.BaseURL != DefaultBaseURL || c.cfg.AuthURL != DefaultAuthURL || c.cfg.MaxTokens != DefaultMaxTokens {
		t.Errorf("config = %+v", c.cfg)
	}

	t.Setenv("GIGACHAT_MAX_TOKENS", "8000")
	if cfg, err := ConfigFromEnv(); err != nil || cfg.MaxTokens != 8000 {
		t.Errorf("GIGACHAT_MAX_TOKENS=8000: MaxTokens = %d, error = %v", cfg.MaxTokens, err)
	}
	for _, bad := range []string{"many", "0", "-1"} {
		t.Setenv("GIGACHAT_MAX_TOKENS", bad)
		if _, err := ConfigFromEnv(); err == nil || !strings.Contains(err.Error(), "GIGACHAT_MAX_TOKENS") {
			t.Errorf("GIGACHAT_MAX_TOKENS=%q: error = %v, want one naming the variable", bad, err)
		}
	}

	if _, err := New(Config{ClientID: "id"}); err == nil {
		t.Error("New() without a secret succeeded")
	}
}

func TestAuthKey(t *testing.T) {
	key := base64.StdEncoding.EncodeToString([]byte("client-id:client-secret"))

	t.Run("sent to OAuth as is", func(t *testing.T) {
		api := &fakeAPI{chat: func(int, chatRequest) chatReply { return ok(`{}`) }}
		srv := httptest.NewServer(api)
		defer srv.Close()
		c, err := New(Config{
			AuthKey:    " " + key + "\n",
			BaseURL:    srv.URL + "/v1",
			AuthURL:    srv.URL + "/oauth",
			HTTPClient: srv.Client(),
		})
		if err != nil {
			t.Fatalf("New() error = %v", err)
		}
		defer c.Close()
		if _, err := c.Complete(context.Background(), request()); err != nil {
			t.Fatalf("Complete() error = %v", err)
		}
		if got := api.oauthReqs[0].Header.Get("Authorization"); got != "Basic "+key {
			t.Errorf("OAuth Authorization = %q, want the key as is", got)
		}
	})

	t.Run("read from the environment", func(t *testing.T) {
		t.Setenv("GIGACHAT_AUTH_KEY", key)
		if cfg, err := ConfigFromEnv(); err != nil || cfg.AuthKey != key {
			t.Errorf("AuthKey = %q, error = %v", cfg.AuthKey, err)
		}
	})

	t.Run("not a key", func(t *testing.T) {
		for _, bad := range []string{"not base64!", base64.StdEncoding.EncodeToString([]byte("no colon"))} {
			if _, err := New(Config{AuthKey: bad}); err == nil || !strings.Contains(err.Error(), "GIGACHAT_AUTH_KEY") {
				t.Errorf("AuthKey %q: error = %v, want one naming GIGACHAT_AUTH_KEY", bad, err)
			}
		}
	})

	t.Run("pasted in place of the secret", func(t *testing.T) {
		_, err := New(Config{ClientID: "client-id", ClientSecret: key})
		if err == nil || !strings.Contains(err.Error(), "GIGACHAT_AUTH_KEY") {
			t.Errorf("error = %v, want a hint to use GIGACHAT_AUTH_KEY", err)
		}
	})
}

func TestNextRefresh(t *testing.T) {
	tests := []struct {
		ttl, want time.Duration
	}{
		{30 * time.Minute, 25 * time.Minute},
		{8 * time.Minute, 4 * time.Minute}, // halfway beats 5 minutes ahead
		{3 * time.Minute, 90 * time.Second},
		{time.Second, time.Second},
	}
	for _, tt := range tests {
		if got := nextRefresh(tt.ttl, refreshBefore); got != tt.want {
			t.Errorf("nextRefresh(%v) = %v, want %v", tt.ttl, got, tt.want)
		}
	}
}

func TestTokenErrorIsNotAModelError(t *testing.T) {
	api := &fakeAPI{chat: func(int, chatRequest) chatReply { return ok(`{}`) }}
	api.oauthStatus.Store(http.StatusBadRequest)
	c := newTestClient(t, api)

	_, err := c.Complete(context.Background(), request())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.op != opToken || apiErr.Status != http.StatusBadRequest {
		t.Fatalf("error = %v, want the OAuth 400", err)
	}
	// Not taken for a rejected schema, not retried, no fallback model: the
	// same token would fail the same way.
	if n := api.oauthCalls.Load(); n != 1 {
		t.Errorf("OAuth called %d times, want 1", n)
	}
	if reqs, _ := api.requests(); len(reqs) != 0 {
		t.Errorf("%d chat requests without a token", len(reqs))
	}
	if len(c.models()) != 2 {
		t.Error("a token error marked the model unavailable")
	}
}

func TestDoesNotRetryUndecodableAnswer(t *testing.T) {
	api := &fakeAPI{chat: func(int, chatRequest) chatReply {
		return chatReply{status: http.StatusOK, body: "<html>proxy error</html>"}
	}}
	c := newTestClient(t, api)

	if _, err := c.Complete(context.Background(), request()); err == nil {
		t.Fatal("Complete() succeeded on an HTML answer")
	}
	if reqs, _ := api.requests(); len(reqs) != 1 {
		t.Errorf("%d requests, want 1: a broken answer is not a transient error", len(reqs))
	}
}

func TestRateLimitDoesNotFallBack(t *testing.T) {
	api := &fakeAPI{chat: func(int, chatRequest) chatReply {
		r := fail(http.StatusTooManyRequests)
		r.header = map[string]string{"Retry-After": "0"}
		return r
	}}
	c := newTestClient(t, api)

	_, err := c.Complete(context.Background(), request())
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusTooManyRequests {
		t.Fatalf("error = %v, want 429", err)
	}
	reqs, _ := api.requests()
	if len(reqs) != maxAttempts {
		t.Errorf("%d requests, want %d retries of the same model", len(reqs), maxAttempts)
	}
	for _, r := range reqs {
		if r.Model != "GigaChat-3-Ultra" {
			t.Errorf("request to %q: the one-request limit is per account, another model does not help", r.Model)
		}
	}
}

func TestBackgroundRefreshStopsAfterExpiry(t *testing.T) {
	api := &fakeAPI{
		tokenTTL: 2 * time.Second,
		chat:     func(int, chatRequest) chatReply { return ok(`{}`) },
	}
	c := newTestClient(t, api)
	c.tokens.minValid = 100 * time.Millisecond
	if _, err := c.Complete(context.Background(), request()); err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	api.oauthStatus.Store(http.StatusServiceUnavailable) // OAuth goes down

	// Retries run while the token lives (about 2s), then stop: an idle
	// client must not poll OAuth forever.
	time.Sleep(3500 * time.Millisecond)
	settled := api.oauthCalls.Load()
	time.Sleep(1500 * time.Millisecond)
	if n := api.oauthCalls.Load(); n != settled {
		t.Errorf("OAuth still polled after the token expired: %d calls, then %d", settled, n)
	}
	if settled < 2 {
		t.Errorf("OAuth called %d times, want refresh attempts before expiry", settled)
	}
}
