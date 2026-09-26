// Package gigachat is a cards.Provider for the GigaChat API, on plain
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
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/tlsroot"
)

// Defaults, overridable through Config or the environment.
const (
	// DefaultBaseURL serves GigaChat-3-Ultra; the older
	// https://gigachat.devices.sberbank.ru/api/v1 does not.
	DefaultBaseURL       = "https://api.giga.chat/v1"
	DefaultAuthURL       = "https://ngw.devices.sberbank.ru:9443/api/v2/oauth"
	DefaultScope         = "GIGACHAT_API_PERS"
	DefaultModel         = "GigaChat-3-Ultra"
	DefaultFallbackModel = "GigaChat-2-Max"
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

// Config configures a Client. ClientID and ClientSecret are required.
type Config struct {
	ClientID      string
	ClientSecret  string
	Scope         string // GIGACHAT_API_PERS for individuals
	Model         string
	FallbackModel string // used when Model is out of quota or failing; empty to disable
	BaseURL       string // up to the API version: https://api.giga.chat/v1
	AuthURL       string
	HTTPClient    *http.Client // nil: a client trusting the Russian Trusted Root CA
	Logger        *slog.Logger
}

// ConfigFromEnv reads GIGACHAT_CLIENT_ID, GIGACHAT_CLIENT_SECRET,
// GIGACHAT_SCOPE, GIGACHAT_MODEL, GIGACHAT_FALLBACK_MODEL, GIGACHAT_BASE_URL
// and GIGACHAT_AUTH_URL. Unset optional values take defaults in New.
func ConfigFromEnv() Config {
	return Config{
		ClientID:      os.Getenv("GIGACHAT_CLIENT_ID"),
		ClientSecret:  os.Getenv("GIGACHAT_CLIENT_SECRET"),
		Scope:         os.Getenv("GIGACHAT_SCOPE"),
		Model:         os.Getenv("GIGACHAT_MODEL"),
		FallbackModel: os.Getenv("GIGACHAT_FALLBACK_MODEL"),
		BaseURL:       os.Getenv("GIGACHAT_BASE_URL"),
		AuthURL:       os.Getenv("GIGACHAT_AUTH_URL"),
	}
}

// Client calls GigaChat chat completions. It is safe for concurrent use, but
// mind the plan: individuals get one concurrent request.
type Client struct {
	cfg     Config
	http    *http.Client
	tokens  *tokenSource
	log     *slog.Logger
	backoff time.Duration

	mu          sync.Mutex
	noStrict    map[string]bool      // models that rejected structured output
	unavailable map[string]time.Time // model → skip until
}

var _ cards.Provider = (*Client)(nil)

// New returns a Client. Call Close when done to stop token refreshing.
func New(cfg Config) (*Client, error) {
	if cfg.ClientID == "" || cfg.ClientSecret == "" {
		return nil, errors.New("gigachat: client id and secret are required")
	}
	cfg.Scope = or(cfg.Scope, DefaultScope)
	cfg.Model = or(cfg.Model, DefaultModel)
	cfg.BaseURL = strings.TrimSuffix(or(cfg.BaseURL, DefaultBaseURL), "/")
	cfg.AuthURL = or(cfg.AuthURL, DefaultAuthURL)
	if cfg.FallbackModel == cfg.Model {
		cfg.FallbackModel = ""
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = newHTTPClient()
	}
	return &Client{
		cfg:         cfg,
		http:        hc,
		tokens:      newTokenSource(hc, cfg.AuthURL, cfg.Scope, cfg.ClientID, cfg.ClientSecret, cfg.Logger),
		log:         cfg.Logger,
		backoff:     backoffBase,
		noStrict:    map[string]bool{},
		unavailable: map[string]time.Time{},
	}, nil
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
	op         string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("gigachat: %s: status %d: %s", e.op, e.Status, e.Body)
}

// Complete sends one chat completion. Transport problems are retried here
// with exponential backoff; if the model stays unavailable, the fallback
// model gets the request. The answer is returned as is: with or without
// structured output, the caller validates it.
func (c *Client) Complete(ctx context.Context, req cards.Request) (cards.Response, error) {
	models := c.models()
	var err error
	for i, model := range models {
		var resp cards.Response
		resp, err = c.completeWith(ctx, model, req)
		if err == nil {
			return resp, nil
		}
		if ctx.Err() != nil || !shouldFallback(err) {
			return cards.Response{}, err
		}
		if isQuotaOrAccess(err) {
			c.markUnavailable(model)
		}
		if i+1 < len(models) {
			c.log.Warn("gigachat: switching to fallback model", "model", model, "fallback", models[i+1], "err", err)
		}
	}
	return cards.Response{}, err
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
func (c *Client) completeWith(ctx context.Context, model string, req cards.Request) (cards.Response, error) {
	strict := len(req.Schema) > 0 && !c.strictOff(model)
	droppedStrict := false
	for attempt := 1; ; attempt++ {
		resp, err := c.call(ctx, model, req, strict)
		if err == nil {
			if droppedStrict {
				c.setStrictOff(model)
			}
			return resp, nil
		}

		var apiErr *APIError
		if strict && errors.As(err, &apiErr) &&
			(apiErr.Status == http.StatusBadRequest || apiErr.Status == http.StatusUnprocessableEntity) {
			c.log.Warn("gigachat: structured output rejected, retrying with the schema in the prompt",
				"model", model, "status", apiErr.Status, "body", apiErr.Body)
			strict, droppedStrict = false, true
			attempt--
			continue
		}
		if ctx.Err() != nil || !retryable(err) || attempt >= maxAttempts {
			return cards.Response{}, err
		}

		wait := c.backoffFor(attempt, err)
		c.log.Warn("gigachat: request failed, retrying",
			"model", model, "attempt", attempt, "wait", wait.Round(time.Millisecond), "err", err)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return cards.Response{}, fmt.Errorf("gigachat: %w (last error: %w)", ctx.Err(), err)
		}
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
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

func (c *Client) call(ctx context.Context, model string, req cards.Request, strict bool) (cards.Response, error) {
	system := req.System
	var format *responseFormat
	switch {
	case strict:
		format = &responseFormat{Type: "json_schema", Schema: req.Schema, Strict: true}
	case len(req.Schema) > 0:
		system += schemaPromptNote + string(req.Schema)
	}

	body, err := json.Marshal(chatRequest{
		Model: model,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: req.User},
		},
		// The API wants temperature > 0; values up to 0.001 switch it to the
		// most deterministic mode, which is what temperature 0 means.
		Temperature:    max(req.Temperature, minTemperature),
		MaxTokens:      req.MaxTokens,
		ResponseFormat: format,
	})
	if err != nil {
		return cards.Response{}, fmt.Errorf("gigachat: marshal request: %w", err)
	}

	token, err := c.tokens.Token(ctx)
	if err != nil {
		return cards.Response{}, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return cards.Response{}, fmt.Errorf("gigachat: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return cards.Response{}, fmt.Errorf("gigachat: chat request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return cards.Response{}, fmt.Errorf("gigachat: read chat response: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		return cards.Response{}, &APIError{
			Status:     resp.StatusCode,
			Body:       clip(respBody),
			RetryAfter: retryAfter(resp.Header),
			op:         "chat " + model,
		}
	}

	var cr chatResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return cards.Response{}, fmt.Errorf("gigachat: decode chat response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return cards.Response{}, fmt.Errorf("gigachat: chat response has no choices: %s", clip(respBody))
	}
	choice := cr.Choices[0]
	if choice.FinishReason == "blacklist" || choice.FinishReason == "length" {
		// Passed on anyway: the caller sees an unusable answer and handles it
		// like any other.
		c.log.Warn("gigachat: answer cut short", "model", model, "finish_reason", choice.FinishReason)
	}
	return cards.Response{
		Content: []byte(choice.Message.Content),
		Usage: cards.Usage{
			PromptTokens:     cr.Usage.PromptTokens,
			CompletionTokens: cr.Usage.CompletionTokens,
		},
		Model: or(cr.Model, model),
	}, nil
}

// retryable: rate limits, server errors and network trouble pass with time.
func retryable(err error) bool {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return apiErr.Status == http.StatusTooManyRequests || apiErr.Status >= 500
	}
	return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// isQuotaOrAccess: the model will not answer this account for a while: no
// tokens left (402), not in the plan (403), unknown model (404).
func isQuotaOrAccess(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Status {
	case http.StatusPaymentRequired, http.StatusForbidden, http.StatusNotFound:
		return true
	}
	return false
}

// shouldFallback: another model may succeed where this one did not. Bad
// requests and bad credentials fail the same on any model.
func shouldFallback(err error) bool {
	return isQuotaOrAccess(err) || retryable(err)
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
