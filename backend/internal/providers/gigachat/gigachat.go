// Package gigachat is a providers.Provider for the GigaChat API, on plain
// net/http.
//
// It handles what is specific to GigaChat: OAuth token exchange, the Russian
// Trusted Root CA, structured output, rate limits and a fallback model. It
// knows nothing about cards: prompts and a schema in, raw text and usage out.
package gigachat

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/tlsroot"
)

const (
	// maxAttempts per model for 429, 5xx and network errors.
	maxAttempts    = 4
	backoffBase    = time.Second
	backoffMax     = 30 * time.Second
	retryAfterMax  = time.Minute
	httpTimeout    = 3 * time.Minute // a safety net; calls are bounded by their context
	maxBodyBytes   = 4 << 20
	logBodyLength  = 500
	modelCooldown  = 10 * time.Minute // how long a model that ran out of quota is skipped
	minTemperature = 0.001
)

// Config configures a Client. Credentials are required: either AuthKey, or
// ClientID and ClientSecret.
type Config struct {
	// AuthKey is the Authorization Key from the project settings in the
	// GigaChat cabinet: base64 of "client_id:client_secret". Takes precedence
	// over ClientID and ClientSecret.
	AuthKey       string
	ClientID      string
	ClientSecret  string
	Scope         string // GIGACHAT_API_PERS for individuals
	Model         string
	FallbackModel string // used when Model is out of quota or failing; empty to disable
	BaseURL       string // up to the API version: https://api.giga.chat/v1
	AuthURL       string
	HTTPClient    *http.Client // nil: a client trusting the Russian Trusted Root CA
	// MaxTokens caps the answer, DefaultMaxTokens by default. It is set here
	// rather than by the caller: token counts depend on the model's tokenizer
	// and price.
	MaxTokens int
}

func env(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// ConfigFromEnv reads GIGACHAT_AUTH_KEY, GIGACHAT_CLIENT_ID,
// GIGACHAT_CLIENT_SECRET, GIGACHAT_SCOPE, GIGACHAT_MODEL,
// GIGACHAT_FALLBACK_MODEL, GIGACHAT_BASE_URL, GIGACHAT_AUTH_URL and
// GIGACHAT_MAX_TOKENS. Unset optional values take defaults in New.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		AuthKey:       env("GIGACHAT_AUTH_KEY"),
		ClientID:      env("GIGACHAT_CLIENT_ID"),
		ClientSecret:  env("GIGACHAT_CLIENT_SECRET"),
		Scope:         env("GIGACHAT_SCOPE"),
		Model:         env("GIGACHAT_MODEL"),
		FallbackModel: env("GIGACHAT_FALLBACK_MODEL"),
		BaseURL:       strings.TrimSuffix(env("GIGACHAT_BASE_URL"), "/"),
		AuthURL:       env("GIGACHAT_AUTH_URL"),
	}

	var errs []error

	for _, r := range []struct{ name, val string }{
		{"GIGACHAT_AUTH_KEY", cfg.AuthKey},
		{"GIGACHAT_CLIENT_ID", cfg.ClientID},
		{"GIGACHAT_CLIENT_SECRET", cfg.ClientSecret},
		{"GIGACHAT_SCOPE", cfg.Scope},
		{"GIGACHAT_MODEL", cfg.Model},
		{"GIGACHAT_FALLBACK_MODEL", cfg.FallbackModel},
		{"GIGACHAT_BASE_URL", cfg.BaseURL},
		{"GIGACHAT_AUTH_URL", cfg.AuthURL},
	} {
		if r.val == "" {
			errs = append(errs, fmt.Errorf("%s is required but empty", r.name))
		}
	}

	// URL should be valid http(s).
	for _, u := range []struct{ name, val string }{
		{"GIGACHAT_BASE_URL", cfg.BaseURL},
		{"GIGACHAT_AUTH_URL", cfg.AuthURL},
	} {
		if u.val == "" {
			continue
		}
		parsed, err := url.Parse(u.val)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
			errs = append(errs, fmt.Errorf("%s = %q, want a valid http(s) URL", u.name, u.val))
		}
	}

	// MAX_TOKENS
	if v := env("GIGACHAT_MAX_TOKENS"); v == "" {
		errs = append(errs, errors.New("GIGACHAT_MAX_TOKENS is required but empty"))
	} else if n, err := strconv.Atoi(v); err != nil || n <= 0 {
		errs = append(errs, fmt.Errorf("GIGACHAT_MAX_TOKENS = %q, want a positive number", v))
	} else {
		cfg.MaxTokens = n
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("gigachat config: %w", errors.Join(errs...))
	}
	return cfg, nil
}

// Client calls GigaChat chat completions. It is safe for concurrent use, but
// mind the plan: individuals get one concurrent request.
type Client struct {
	cfg     Config
	http    *http.Client
	tokens  *tokenSource
	backoff time.Duration

	mu          sync.Mutex
	noStrict    map[string]bool      // models that rejected structured output
	unavailable map[string]time.Time // model → skip until
}

var _ providers.Provider = (*Client)(nil)

// New returns a Client. Call Close when done to stop token refreshing.
func New(cfg Config) (*Client, error) {
	authKey, err := authorizationKey(cfg)
	if err != nil {
		return nil, err
	}
	if cfg.FallbackModel == cfg.Model {
		cfg.FallbackModel = ""
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = newHTTPClient()
	}
	return &Client{
		cfg:         cfg,
		http:        hc,
		tokens:      newTokenSource(hc, cfg.AuthURL, cfg.Scope, authKey),
		backoff:     backoffBase,
		noStrict:    map[string]bool{},
		unavailable: map[string]time.Time{},
	}, nil
}

// authorizationKey returns the Basic credentials for the OAuth exchange: the
// Authorization Key as the cabinet shows it, or one built from the client id
// and secret.
func authorizationKey(cfg Config) (string, error) {
	if key := strings.TrimPrefix(strings.TrimSpace(cfg.AuthKey), "Basic "); key != "" {
		if raw, err := base64.StdEncoding.DecodeString(key); err != nil || !strings.Contains(string(raw), ":") {
			return "", errors.New("gigachat: AuthKey (GIGACHAT_AUTH_KEY) is not an Authorization Key: base64 of client_id:client_secret expected")
		}
		return key, nil
	}
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return "", errors.New("gigachat: credentials required: AuthKey (GIGACHAT_AUTH_KEY), or ClientID and ClientSecret (GIGACHAT_CLIENT_ID, GIGACHAT_CLIENT_SECRET)")
	}
	// The Authorization Key pasted in place of the secret is an easy slip: it
	// would be encoded a second time and OAuth would answer a bare 401.
	if raw, err := base64.StdEncoding.DecodeString(cfg.ClientSecret); err == nil && strings.HasPrefix(string(raw), cfg.ClientID+":") {
		return "", errors.New("gigachat: ClientSecret (GIGACHAT_CLIENT_SECRET) holds the Authorization Key; set it as AuthKey (GIGACHAT_AUTH_KEY) instead")
	}
	return base64.StdEncoding.EncodeToString([]byte(cfg.ClientID + ":" + cfg.ClientSecret)), nil
}

// Close stops the background token refresh.
func (c *Client) Close() {
	c.tokens.Close()
}

func newHTTPClient() *http.Client {
	return &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			TLSClientConfig: &tls.Config{
				RootCAs:    tlsroot.Pool(),
				MinVersion: tls.VersionTLS12,
			},
			MaxIdleConns:        10,
			MaxIdleConnsPerHost: 4,
			IdleConnTimeout:     90 * time.Second,
			TLSHandshakeTimeout: 15 * time.Second,
		},
		Timeout: httpTimeout,
	}
}

// APIError is a non-2xx answer from GigaChat.
type APIError struct {
	Status     int
	Body       string
	RetryAfter time.Duration
	op         string // opToken or "chat <model>"
}

// opToken marks errors of the OAuth exchange: they say nothing about the
// model or the request, and the same token serves every model.
const opToken = "token"

func (e *APIError) Error() string {
	return fmt.Sprintf("gigachat: %s: status %d: %s", e.op, e.Status, e.Body)
}

// chatError returns the APIError of a chat completion call in err's chain,
// or nil for anything else, token errors included.
func chatError(err error) *APIError {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.op != opToken {
		return apiErr
	}
	return nil
}

// transportError is a failure to reach GigaChat or to read its answer, the
// only kind of non-HTTP error that may pass on a retry.
type transportError struct{ err error }

func (e transportError) Error() string { return e.err.Error() }
func (e transportError) Unwrap() error { return e.err }

// Complete sends one chat completion. Transport problems are retried here
// with exponential backoff; if the model stays unavailable, the fallback
// model gets the request. The answer is returned as is: with or without
// structured output, the caller validates it.
func (c *Client) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	var fileID string
	if req.Image != nil {
		id, err := c.upload(ctx, req.Image)
		if err != nil {
			return providers.Response{}, err
		}
		fileID = id
		defer c.deleteFile(id)
	}

	models := c.models()
	var err error
	for i, model := range models {
		var resp providers.Response
		resp, err = c.completeWith(ctx, model, req, fileID)
		if err == nil {
			return resp, nil
		}
		if ctx.Err() != nil || !shouldFallback(err) {
			return providers.Response{}, err
		}
		if isQuotaOrAccess(err) {
			c.markUnavailable(model)
		}
		if i+1 < len(models) {
			slog.Warn("gigachat: switching to fallback model", "model", model, "fallback", models[i+1], "err", err)
		}
	}
	return providers.Response{}, err
}

// models lists the models to try in order, skipping one that recently ran
// out of quota.
func (c *Client) models() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	primary, fallback := c.cfg.Model, c.cfg.FallbackModel
	if fallback == "" {
		return []string{primary}
	}
	if until, ok := c.unavailable[primary]; ok {
		if time.Now().Before(until) {
			return []string{fallback}
		}
		delete(c.unavailable, primary)
	}
	return []string{primary, fallback}
}

func (c *Client) markUnavailable(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.unavailable[model] = time.Now().Add(modelCooldown)
}

func (c *Client) strictOff(model string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.noStrict[model]
}

func (c *Client) setStrictOff(model string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.noStrict[model] = true
}

// completeWith calls one model, retrying 429, 5xx and network errors.
//
// Structured output (response_format with strict) is not confirmed for every
// model. If the API rejects it with 400 or 422, the same request goes again
// without it, the schema moved into the system prompt; when that works, the
// model is remembered and later calls skip straight to the plain form.
func (c *Client) completeWith(ctx context.Context, model string, req providers.Request, fileID string) (providers.Response, error) {
	strict := len(req.Schema) > 0 && !c.strictOff(model)
	droppedStrict := false
	for attempt := 1; ; attempt++ {
		resp, err := c.call(ctx, model, req, strict, fileID)
		if err == nil {
			if droppedStrict {
				c.setStrictOff(model)
			}
			return resp, nil
		}

		if apiErr := chatError(err); strict && apiErr != nil &&
			(apiErr.Status == http.StatusBadRequest || apiErr.Status == http.StatusUnprocessableEntity) {
			slog.Warn("gigachat: structured output rejected, retrying with the schema in the prompt",
				"model", model, "status", apiErr.Status, "body", apiErr.Body)
			strict, droppedStrict = false, true
			attempt--
			continue
		}
		if ctx.Err() != nil || !retryable(err) || attempt >= maxAttempts {
			return providers.Response{}, err
		}

		wait := c.backoffFor(attempt, err)
		slog.Warn("gigachat: request failed, retrying",
			"model", model, "attempt", attempt, "wait", wait.Round(time.Millisecond), "err", err)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return providers.Response{}, fmt.Errorf("gigachat: %w (last error: %w)", ctx.Err(), err)
		}
	}
}

type chatMessage struct {
	Role        string   `json:"role"`
	Content     string   `json:"content"`
	Attachments []string `json:"attachments,omitempty"` // ids of uploaded files
}

// responseFormat is structured output. On /v1/chat/completions it is a
// top-level field; /v2 nests it in model_options.
type responseFormat struct {
	Type   string          `json:"type"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []chatMessage   `json:"messages"`
	Temperature    float64         `json:"temperature"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Stream         bool            `json:"stream"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
}

type chatResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

const schemaPromptNote = "\n\nОтвет — только JSON, соответствующий этой JSON Schema:\n"

func (c *Client) call(ctx context.Context, model string, req providers.Request, strict bool, fileID string) (providers.Response, error) {
	system := req.System
	var format *responseFormat
	switch {
	case strict:
		format = &responseFormat{Type: "json_schema", Schema: req.Schema, Strict: true}
	case len(req.Schema) > 0:
		system += schemaPromptNote + string(req.Schema)
	}
	user := chatMessage{Role: "user", Content: req.User}
	if fileID != "" {
		user.Attachments = []string{fileID}
	}

	body, err := json.Marshal(chatRequest{
		Model:    model,
		Messages: []chatMessage{{Role: "system", Content: system}, user},
		// The API wants temperature > 0; values up to 0.001 switch it to the
		// most deterministic mode, which is what temperature 0 means.
		Temperature:    max(req.Temperature, minTemperature),
		MaxTokens:      c.cfg.MaxTokens,
		ResponseFormat: format,
	})
	if err != nil {
		return providers.Response{}, fmt.Errorf("gigachat: marshal request: %w", err)
	}

	token, err := c.tokens.Token(ctx)
	if err != nil {
		return providers.Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return providers.Response{}, fmt.Errorf("gigachat: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return providers.Response{}, fmt.Errorf("gigachat: chat request: %w", transportError{err})
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return providers.Response{}, fmt.Errorf("gigachat: read chat response: %w", transportError{err})
	}
	if resp.StatusCode != http.StatusOK {
		return providers.Response{}, &APIError{
			Status:     resp.StatusCode,
			Body:       clip(respBody),
			RetryAfter: retryAfter(resp.Header),
			op:         "chat " + model,
		}
	}

	var cr chatResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return providers.Response{}, fmt.Errorf("gigachat: decode chat response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return providers.Response{}, fmt.Errorf("gigachat: chat response has no choices: %s", clip(respBody))
	}
	choice := cr.Choices[0]
	switch choice.FinishReason {
	case "blacklist":
		// The content filter answered instead of the model: history and
		// politics trip it now and then. Another model may pass the text.
		return providers.Response{}, fmt.Errorf("gigachat: %s: %w", model, providers.ErrRefused)
	case "length":
		// Passed on anyway: the caller sees an unusable answer and handles it
		// like any other.
		slog.Warn("gigachat: answer cut short", "model", model, "finish_reason", choice.FinishReason)
	}
	return providers.Response{
		Content: []byte(choice.Message.Content),
		Usage: providers.Usage{
			PromptTokens:     cr.Usage.PromptTokens,
			CompletionTokens: cr.Usage.CompletionTokens,
		},
		Model: or(cr.Model, model),
	}, nil
}

// retryable: rate limits, server errors and network trouble pass with time.
// A malformed request, an undecodable answer or bad credentials do not.
func retryable(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusTooManyRequests || apiErr.Status >= 500
	}
	var te transportError
	return errors.As(err, &te)
}

// isQuotaOrAccess: the model will not answer this account for a while: no
// tokens left (402), not in the plan (403), unknown model (404).
func isQuotaOrAccess(err error) bool {
	apiErr := chatError(err)
	if apiErr == nil {
		return false
	}
	switch apiErr.Status {
	case http.StatusPaymentRequired, http.StatusForbidden, http.StatusNotFound:
		return true
	}
	return false
}

// shouldFallback: another model may succeed where this one failed, because
// this one is out of quota, not available, keeps failing on the server, or
// its content filter blocked the text. Not for rate limits (the one-request
// limit is per account), network trouble (same host), token errors (same
// token) or bad requests.
func shouldFallback(err error) bool {
	if errors.Is(err, providers.ErrRefused) {
		return true
	}
	apiErr := chatError(err)
	return apiErr != nil && (isQuotaOrAccess(err) || apiErr.Status >= 500)
}

func (c *Client) backoffFor(attempt int, err error) time.Duration {
	var apiErr *APIError
	if errors.As(err, &apiErr) && apiErr.RetryAfter > 0 {
		return min(apiErr.RetryAfter, retryAfterMax)
	}
	d := min(c.backoff<<(attempt-1), backoffMax)
	// ±20% jitter so parallel clients do not retry in lockstep.
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}

func retryAfter(h http.Header) time.Duration {
	v := h.Get("Retry-After")
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		return max(time.Until(t), 0)
	}
	return 0
}

func clip(b []byte) string {
	s := strings.TrimSpace(string(b))
	if r := []rune(s); len(r) > logBodyLength {
		return string(r[:logBodyLength]) + "…"
	}
	return s
}

func or(v, def string) string {
	if v == "" {
		return def
	}
	return v
}
