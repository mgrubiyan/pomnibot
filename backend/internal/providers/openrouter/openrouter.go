// Package openrouter implements the shared model interface using OpenRouter's
// Chat Completions API. Prompts and document processing belong to the caller.
package openrouter

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg" // Decode image dimensions for conservative vision reservations.
	_ "image/png"  // Decode image dimensions for conservative vision reservations.
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// Defaults can be overridden independently of the legacy providers.
const (
	DefaultModel       = "anthropic/claude-haiku-5.5"
	DefaultBaseURL     = "https://openrouter.ai/api/v1"
	DefaultMaxTokens   = 50000
	DefaultEffort      = "high"
	maxAttempts        = 3
	maxBodyBytes       = 4 << 20
	maxPromptPrice     = .1 // USD per million; also used by the preflight reservation
	maxCompletionPrice = .5
)

// Config holds credentials, the model and optional HTTP transport injection.
type Config struct {
	APIKey     string
	Model      string
	Effort     string // empty or none disables thinking; explicit levels enable it
	BaseURL    string
	MaxTokens  int
	HTTPClient *http.Client
}

// ConfigFromEnv reads OPENROUTER_KEY and the optional OPENROUTER_* settings.
func ConfigFromEnv() (Config, error) {
	get := func(key, fallback string) string {
		if s := strings.TrimSpace(os.Getenv(key)); s != "" {
			return s
		}
		return fallback
	}
	cfg := Config{APIKey: get("OPENROUTER_KEY", ""), Model: get("OPENROUTER_MODEL", DefaultModel), BaseURL: strings.TrimRight(get("OPENROUTER_BASE_URL", DefaultBaseURL), "/"), MaxTokens: DefaultMaxTokens, Effort: get("OPENROUTER_EFFORT", DefaultEffort)}
	n, err := strconv.Atoi(get("OPENROUTER_MAX_TOKENS", strconv.Itoa(DefaultMaxTokens)))
	if err != nil || n <= 0 {
		return Config{}, errors.New("OPENROUTER_MAX_TOKENS must be a positive integer")
	}
	cfg.MaxTokens = n
	if cfg.APIKey == "" {
		return Config{}, errors.New("OPENROUTER_KEY is required")
	}
	if !validURL(cfg.BaseURL) {
		return Config{}, errors.New("OPENROUTER_BASE_URL must be an http(s) URL without credentials, query or fragment")
	}
	return cfg, nil
}

func validURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != "" && u.User == nil && u.RawQuery == "" && u.Fragment == ""
}

// Client is safe for concurrent use. It never logs prompts or API error bodies.
type Client struct {
	cfg     Config
	http    *http.Client
	backoff time.Duration
}

var _ providers.Provider = (*Client)(nil)

// New validates configuration without making a network request.
func New(cfg Config) (*Client, error) {
	if strings.TrimSpace(cfg.APIKey) == "" || cfg.Model == "" || cfg.MaxTokens <= 0 || !validURL(cfg.BaseURL) {
		return nil, errors.New("openrouter: APIKey, Model, valid BaseURL and positive MaxTokens are required")
	}
	switch cfg.Effort {
	case "none":
		cfg.Effort = ""
	case "", "low", "medium", "high", "xhigh", "max":
	default:
		return nil, errors.New("openrouter: unsupported effort")
	}
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 5 * time.Minute, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
		// Recovery must not close idle connections belonging to other services.
		if transport, ok := http.DefaultTransport.(*http.Transport); ok {
			hc.Transport = newRecoveryTransport(transport)
		}
	}
	return &Client{cfg: cfg, http: hc, backoff: time.Second}, nil
}

// APIError intentionally omits upstream error text, which may echo private data.
type APIError struct {
	Status     int
	retryAfter time.Duration
}

func (e *APIError) Error() string { return fmt.Sprintf("openrouter: status %d", e.Status) }

type transportError struct{}

func (transportError) Error() string { return "openrouter: network or response read failure" }

func retryable(err error) bool {
	var api *APIError
	if errors.As(err, &api) {
		return api.Status == 408 || api.Status == 429 || api.Status >= 500
	}
	var transport transportError
	return errors.As(err, &transport)
}

func unavailable(err error) bool {
	var api *APIError
	return retryable(err) || (errors.As(err, &api) && (api.Status == 401 || api.Status == 402 || api.Status == 403 || api.Status == 404))
}

// Complete retries temporary failures, respecting both cancellation and Retry-After.
func (c *Client) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	for attempt := 0; ; attempt++ {
		res, err := c.call(ctx, req)
		if err == nil {
			return res, nil
		}
		if ctx.Err() != nil {
			return res, ctx.Err()
		}
		if !retryable(err) || attempt == maxAttempts-1 {
			if unavailable(err) {
				err = fmt.Errorf("%w: %w", err, providers.ErrUnavailable)
			}
			return res, err
		}
		wait := time.Duration(float64(c.backoff<<attempt) * (0.8 + 0.4*rand.Float64()))
		var api *APIError
		if errors.As(err, &api) {
			wait = max(wait, api.retryAfter)
		}
		slog.Warn("openrouter: retrying", "attempt", attempt+1, "wait", wait, "err", err)
		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return res, ctx.Err()
		case <-timer.C:
		}
	}
}

type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type imageURL struct {
	URL string `json:"url"`
}
type part struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}
type schema struct {
	Strict bool            `json:"strict"`
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
}
type responseFormat struct {
	Type       string `json:"type"`
	JSONSchema schema `json:"json_schema"`
}
type chatRequest struct {
	Stream         bool            `json:"stream"`
	Model          string          `json:"model"`
	Messages       []message       `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Reasoning      struct {
		Enabled bool   `json:"enabled"`
		Effort  string `json:"effort,omitempty"`
	} `json:"reasoning"`
	Provider struct {
		RequireParameters bool               `json:"require_parameters"`
		MaxPrice          map[string]float64 `json:"max_price,omitempty"`
	} `json:"provider"`
}
type chatResponse struct {
	ID       string           `json:"id"`
	Provider string           `json:"provider"`
	Model    string           `json:"model"`
	Error    *responseError   `json:"error"`
	Choices  []responseChoice `json:"choices"`
	Usage    responseUsage    `json:"usage"`
}

type responseMessage struct {
	Content *string `json:"content"`
	Refusal *string `json:"refusal"`
}
type responseChoice struct {
	Index        int             `json:"index"`
	Message      responseMessage `json:"message"`
	Delta        responseMessage `json:"delta"`
	FinishReason string          `json:"finish_reason"`
	Error        *responseError  `json:"error"`
}
type responseUsage struct {
	PromptTokens      int      `json:"prompt_tokens"`
	CompletionTokens  int      `json:"completion_tokens"`
	Cost              *float64 `json:"cost"`
	CompletionDetails struct {
		ReasoningTokens *int `json:"reasoning_tokens"`
	} `json:"completion_tokens_details"`
}
type responseError struct {
	Code json.RawMessage `json:"code"`
}

func (e *responseError) status() int {
	var code int
	if json.Unmarshal(e.Code, &code) == nil && code >= 400 && code <= 599 {
		return code
	}
	var name string
	_ = json.Unmarshal(e.Code, &name)
	switch name {
	case "rate_limit_exceeded":
		return 429
	case "authentication":
		return 401
	case "permission_denied":
		return 403
	case "payment_required":
		return 402
	case "invalid_request", "context_length_exceeded":
		return 400
	default:
		return 502
	}
}

func (c *Client) call(ctx context.Context, req providers.Request) (result providers.Response, callErr error) {
	var user any = req.User
	if images := providers.RequestImages(req); len(images) > 0 {
		parts := []part{{Type: "text", Text: req.User}}
		for _, img := range images {
			parts = append(parts, part{Type: "image_url", ImageURL: &imageURL{URL: "data:" + img.MimeType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)}})
		}
		user = parts
	}

	body := chatRequest{Stream: true, Model: c.cfg.Model, Messages: []message{{Role: "system", Content: req.System}, {Role: "user", Content: user}}, MaxTokens: c.cfg.MaxTokens}
	// Haiku 5.5 removed sampling parameters. With require_parameters enabled,
	// even temperature=0 excludes every endpoint and OpenRouter returns 404.
	baseModel, _, _ := strings.Cut(c.cfg.Model, ":")
	if baseModel != "anthropic/claude-haiku-5.5" && baseModel != "~anthropic/claude-haiku-latest" {
		body.Temperature = &req.Temperature
	}
	body.Reasoning.Enabled = c.cfg.Effort != ""
	body.Reasoning.Effort = c.cfg.Effort
	body.Provider.RequireParameters = true
	if b := providers.BudgetFrom(ctx); b != nil && b.MonetaryLimited() {
		body.Provider.MaxPrice = map[string]float64{"prompt": maxPromptPrice, "completion": maxCompletionPrice}
	}
	if len(req.Schema) > 0 {
		body.ResponseFormat = &responseFormat{Type: "json_schema", JSONSchema: schema{Strict: true, Name: "answer", Schema: req.Schema}}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return result, errors.New("openrouter: invalid request schema")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return result, errors.New("openrouter: invalid request URL")
	}
	r.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "text/event-stream, application/json")
	if b := providers.BudgetFrom(ctx); b != nil && b.MonetaryLimited() && c.inputBound(req) >= 100000 {
		return result, errors.New("openrouter: request exceeds bounded input tier")
	}
	charge := providers.Usage{}
	chargeKnown := false
	finish, reserveErr := providers.ReserveCall(ctx, c.costBound(req))
	if reserveErr != nil {
		return result, reserveErr
	}
	defer func() { finish(charge, chargeKnown) }()
	traceCtx, trace := traceRequest(ctx)
	r = r.WithContext(traceCtx)
	began := time.Now()
	resp, err := c.http.Do(r)
	if err != nil {
		c.http.CloseIdleConnections()
		slog.With(trace.attributes()...).Warn("openrouter: request failed", "phase", trace.label(), "took", time.Since(began), "timeout", errors.Is(err, context.DeadlineExceeded))
		return result, transportError{}
	}
	defer func() { _ = resp.Body.Close() }()
	log := slog.With(trace.attributes()...).With("status", resp.StatusCode, "protocol", resp.Proto,
		"request_id", resp.Header.Get("X-Request-Id"), "generation_id", resp.Header.Get("X-Generation-Id"), "cf_ray", resp.Header.Get("Cf-Ray"))
	if resp.StatusCode != http.StatusOK {
		return result, &APIError{Status: resp.StatusCode, retryAfter: retryDelay(resp.Header.Get("Retry-After"))}
	}
	trace.phase.Store(traceReadingBody)
	var cr chatResponse
	var progress streamProgress
	streaming := strings.HasPrefix(strings.ToLower(resp.Header.Get("Content-Type")), "text/event-stream")
	if streaming {
		cr, progress, err = readStream(resp.Body, began, log)
	} else {
		data, err = io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
		progress.Bytes = int64(len(data))
		progress.Complete = json.Valid(data)
		if err == nil && len(data) > maxBodyBytes {
			err = errResponseSize
		}
		if err == nil && json.Unmarshal(data, &cr) != nil {
			err = errResponseJSON
		}
	}
	if cr.Usage.Cost != nil {
		chargeKnown = true
		charge = providers.Usage{PromptTokens: cr.Usage.PromptTokens, CompletionTokens: cr.Usage.CompletionTokens, ReasoningTokens: cr.Usage.CompletionDetails.ReasoningTokens, CostUSD: *cr.Usage.Cost}
	}
	result.Model = cr.Model
	if result.Model == "" {
		result.Model = c.cfg.Model
	}
	result.Usage = providers.Usage{PromptTokens: cr.Usage.PromptTokens, CompletionTokens: cr.Usage.CompletionTokens, ReasoningTokens: cr.Usage.CompletionDetails.ReasoningTokens, CostUSD: charge.CostUSD}
	if len(cr.Choices) > 0 {
		result.Truncated = cr.Choices[0].FinishReason == "length"
	}
	if err != nil {
		// Release this stream before discarding idle connections, so a sequential
		// retry cannot silently reuse the connection on which the reply stalled.
		_ = resp.Body.Close()
		c.http.CloseIdleConnections()
		log.Warn("openrouter: response read failed", "phase", trace.label(), "took", time.Since(began), "timeout", errors.Is(err, context.DeadlineExceeded),
			"response_bytes", progress.Bytes, "response_json_complete", progress.Complete, "streaming", streaming, "stream_events", progress.Events)
		if errors.Is(err, errResponseSize) || errors.Is(err, errStreamProtocol) || errors.Is(err, errResponseJSON) {
			return result, err
		}
		var api *APIError
		if errors.As(err, &api) {
			return result, err
		}
		return result, transportError{}
	}
	if cr.Error != nil {
		return result, &APIError{Status: cr.Error.status()}
	}
	if len(cr.Choices) == 0 {
		return result, errors.New("openrouter: response has no choices")
	}
	choice := cr.Choices[0]
	if choice.Error != nil {
		return result, &APIError{Status: choice.Error.status()}
	}
	if choice.FinishReason == "error" {
		return result, &APIError{Status: http.StatusBadGateway}
	}
	if choice.FinishReason == "content_filter" || choice.Message.Refusal != nil {
		return result, providers.ErrRefused
	}
	if choice.Message.Content == nil || strings.TrimSpace(*choice.Message.Content) == "" {
		return result, errors.New("openrouter: response has no content")
	}
	model := cr.Model
	if model == "" {
		model = c.cfg.Model
	}
	reasoningTokens := any("unreported")
	if cr.Usage.CompletionDetails.ReasoningTokens != nil {
		reasoningTokens = *cr.Usage.CompletionDetails.ReasoningTokens
	}
	log.Info("openrouter: completed", "response_generation_id", cr.ID, "provider", cr.Provider, "model", model, "prompt_tokens", cr.Usage.PromptTokens, "completion_tokens", cr.Usage.CompletionTokens, "reasoning_tokens", reasoningTokens, "cost_usd", charge.CostUSD,
		"response_bytes", progress.Bytes, "response_json_complete", true, "streaming", streaming, "stream_events", progress.Events, "first_token_after_ms", float64(progress.FirstToken)/float64(time.Millisecond), "took", time.Since(began))
	return providers.Response{Content: []byte(*choice.Message.Content), Model: model, Usage: providers.Usage{PromptTokens: cr.Usage.PromptTokens, CompletionTokens: cr.Usage.CompletionTokens, ReasoningTokens: cr.Usage.CompletionDetails.ReasoningTokens, CostUSD: charge.CostUSD}, Truncated: choice.FinishReason == "length"}, nil
}

func retryDelay(s string) time.Duration {
	if n, err := strconv.Atoi(s); err == nil && n > 0 {
		return min(time.Duration(min(n, 60))*time.Second, time.Minute)
	}
	if date, err := http.ParseTime(s); err == nil {
		return max(0, min(time.Until(date), time.Minute))
	}
	return 0
}

// costBound deliberately counts every UTF-8 byte as a token, plus a generous
// image allowance. Routing enforces the corresponding price ceiling. Unknown
// image formats are charged by encoded size instead of assuming cheap vision.
func (c *Client) costBound(r providers.Request) float64 {
	return float64(c.inputBound(r))*maxPromptPrice/1e6 + float64(c.cfg.MaxTokens)*maxCompletionPrice/1e6
}
func (c *Client) inputBound(r providers.Request) int {
	tokens := len(r.System) + len(r.User) + len(r.Schema) + 2048
	for _, img := range providers.RequestImages(r) {
		dims, _, err := image.DecodeConfig(bytes.NewReader(img.Data))
		if err != nil {
			tokens += max(32768, len(img.Data))
			continue
		}
		// Haiku scales images and charges vision tokens by pixel area. This is
		// deliberately much larger than the normal image tokenization allowance.
		tokens += max(4096, (dims.Width*dims.Height+127)/128)
	}
	return tokens
}
