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
	DefaultModel     = "anthropic/claude-haiku-5.5"
	DefaultBaseURL   = "https://openrouter.ai/api/v1"
	DefaultMaxTokens = 8000
	maxAttempts      = 3
	maxBodyBytes     = 4 << 20
)

// Config holds credentials, the model and optional HTTP transport injection.
type Config struct {
	APIKey     string
	Model      string
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
	cfg := Config{APIKey: get("OPENROUTER_KEY", ""), Model: get("OPENROUTER_MODEL", DefaultModel), BaseURL: strings.TrimRight(get("OPENROUTER_BASE_URL", DefaultBaseURL), "/"), MaxTokens: DefaultMaxTokens}
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
	cfg.BaseURL = strings.TrimRight(cfg.BaseURL, "/")
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 3 * time.Minute, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
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
			return providers.Response{}, ctx.Err()
		}
		if !retryable(err) || attempt == maxAttempts-1 {
			if unavailable(err) {
				err = fmt.Errorf("%w: %w", err, providers.ErrUnavailable)
			}
			return providers.Response{}, err
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
			return providers.Response{}, ctx.Err()
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
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
}
type responseFormat struct {
	Type       string `json:"type"`
	JSONSchema schema `json:"json_schema"`
}
type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []message       `json:"messages"`
	Temperature    *float64        `json:"temperature,omitempty"`
	MaxTokens      int             `json:"max_tokens"`
	ResponseFormat *responseFormat `json:"response_format,omitempty"`
	Reasoning      struct {
		Enabled bool `json:"enabled"`
	} `json:"reasoning"`
	Provider struct {
		RequireParameters bool `json:"require_parameters"`
	} `json:"provider"`
}
type chatResponse struct {
	Model string `json:"model"`
	Error *struct {
		Code int `json:"code"`
	} `json:"error"`
	Choices []struct {
		Message struct {
			Content *string `json:"content"`
			Refusal *string `json:"refusal"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int     `json:"prompt_tokens"`
		CompletionTokens int     `json:"completion_tokens"`
		Cost             float64 `json:"cost"`
	} `json:"usage"`
}

func (c *Client) call(ctx context.Context, req providers.Request) (providers.Response, error) {
	var user any = req.User
	if req.Image != nil {
		user = []part{{Type: "text", Text: req.User}, {Type: "image_url", ImageURL: &imageURL{URL: "data:" + req.Image.MimeType + ";base64," + base64.StdEncoding.EncodeToString(req.Image.Data)}}}
	}
	body := chatRequest{Model: c.cfg.Model, Messages: []message{{Role: "system", Content: req.System}, {Role: "user", Content: user}}, MaxTokens: c.cfg.MaxTokens}
	// Haiku 5.5 removed sampling parameters. With require_parameters enabled,
	// even temperature=0 excludes every endpoint and OpenRouter returns 404.
	baseModel, _, _ := strings.Cut(c.cfg.Model, ":")
	if baseModel != "anthropic/claude-haiku-5.5" && baseModel != "~anthropic/claude-haiku-latest" {
		body.Temperature = &req.Temperature
	}
	body.Reasoning.Enabled = false
	body.Provider.RequireParameters = true
	if len(req.Schema) > 0 {
		body.ResponseFormat = &responseFormat{Type: "json_schema", JSONSchema: schema{Name: "answer", Schema: req.Schema}}
	}
	data, err := json.Marshal(body)
	if err != nil {
		return providers.Response{}, errors.New("openrouter: invalid request schema")
	}
	r, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/chat/completions", bytes.NewReader(data))
	if err != nil {
		return providers.Response{}, errors.New("openrouter: invalid request URL")
	}
	r.Header.Set("Authorization", "Bearer "+c.cfg.APIKey)
	r.Header.Set("Content-Type", "application/json")
	r.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(r)
	if err != nil {
		return providers.Response{}, transportError{}
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return providers.Response{}, &APIError{Status: resp.StatusCode, retryAfter: retryDelay(resp.Header.Get("Retry-After"))}
	}
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes+1))
	if err != nil {
		return providers.Response{}, transportError{}
	}
	if len(data) > maxBodyBytes {
		return providers.Response{}, errors.New("openrouter: response exceeds size limit")
	}
	var cr chatResponse
	if json.Unmarshal(data, &cr) != nil {
		return providers.Response{}, errors.New("openrouter: invalid response JSON")
	}
	if cr.Error != nil {
		return providers.Response{}, &APIError{Status: cr.Error.Code}
	}
	if len(cr.Choices) == 0 {
		return providers.Response{}, errors.New("openrouter: response has no choices")
	}
	choice := cr.Choices[0]
	if choice.FinishReason == "content_filter" || choice.Message.Refusal != nil {
		return providers.Response{}, providers.ErrRefused
	}
	if choice.Message.Content == nil || strings.TrimSpace(*choice.Message.Content) == "" {
		return providers.Response{}, errors.New("openrouter: response has no content")
	}
	model := cr.Model
	if model == "" {
		model = c.cfg.Model
	}
	slog.Info("openrouter: completed", "model", model, "prompt_tokens", cr.Usage.PromptTokens, "completion_tokens", cr.Usage.CompletionTokens, "cost_usd", cr.Usage.Cost)
	return providers.Response{Content: []byte(*choice.Message.Content), Model: model, Usage: providers.Usage{PromptTokens: cr.Usage.PromptTokens, CompletionTokens: cr.Usage.CompletionTokens, CostUSD: cr.Usage.Cost}, Truncated: choice.FinishReason == "length"}, nil
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
