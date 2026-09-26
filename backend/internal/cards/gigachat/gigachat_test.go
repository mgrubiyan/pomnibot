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
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/cards"
)

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
	default:
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
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	c.backoff = time.Millisecond
	t.Cleanup(c.Close)
	return c
}

var schema = json.RawMessage(`{"type":"object","properties":{"cards":{"type":"array"}},"required":["cards"]}`)

func request() cards.Request {
	return cards.Request{System: "Правила.", User: "Фрагмент.", Schema: schema, MaxTokens: 1700}
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
	if req.Model != "GigaChat-3-Ultra" || req.Stream || req.MaxTokens != 1700 {
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

	// No requests in between: the timer alone must replace the token.
	deadline := time.Now().Add(3 * time.Second)
	for api.oauthCalls.Load() < 2 {
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

	c, err := New(ConfigFromEnv())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	defer c.Close()
	if c.cfg.Scope != DefaultScope || c.cfg.Model != DefaultModel || c.cfg.FallbackModel != "GigaChat-2-Max" ||
		c.cfg.BaseURL != DefaultBaseURL || c.cfg.AuthURL != DefaultAuthURL {
		t.Errorf("config = %+v", c.cfg)
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
			Logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
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
		if got := ConfigFromEnv().AuthKey; got != key {
			t.Errorf("AuthKey = %q", got)
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
