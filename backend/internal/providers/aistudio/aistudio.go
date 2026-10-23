// Package aistudio is a providers.Provider for the open models of Yandex AI
// Studio, such as Qwen, on its OpenAI-compatible chat completions API, plain
// net/http.
//
// Qwen3.6 writes the cards, grades answers and reads photos of handwritten
// notes, in the same Yandex Cloud folder as the OCR.
package aistudio

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

	"github.com/mgrubiyan/pomnibot/backend/internal/prompts"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/tlsroot"
)

// Defaults for the optional settings.
const (
	DefaultModel     = "qwen3.6-35b-a3b"
	DefaultBaseURL   = "https://ai.api.cloud.yandex.net"
	DefaultMaxTokens = 8000
)

const (
	maxAttempts   = 3
	backoffBase   = time.Second
	backoffMax    = 10 * time.Second
	httpTimeout   = 3 * time.Minute // a safety net; calls are bounded by their context
	maxBodyBytes  = 4 << 20
	logBodyLength = 500
)

// Config configures a Client. All fields but HTTPClient are required.
type Config struct {
	APIKey   string // an API key of a service account with ai.languageModels.user
	FolderID string
	// Model is the model name, such as qwen3.6-35b-a3b, or a full URI:
	// gpt://<folder>/qwen3.6-35b-a3b.
	Model      string
	BaseURL    string       // https://ai.api.cloud.yandex.net
	HTTPClient *http.Client // nil: a client with the system roots plus the Russian Trusted Root CA
	MaxTokens  int          // caps the answer
}

func env(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// ConfigFromEnv reads YC_API_KEY and YC_FOLDER_ID, shared with the OCR, and
// optionally YC_LLM_MODEL, YC_LLM_BASE_URL and YC_LLM_MAX_TOKENS.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		APIKey:    env("YC_API_KEY"),
		FolderID:  env("YC_FOLDER_ID"),
		Model:     env("YC_LLM_MODEL"),
		BaseURL:   strings.TrimSuffix(env("YC_LLM_BASE_URL"), "/"),
		MaxTokens: DefaultMaxTokens,
	}
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}

	var errs []error
	for _, r := range []struct{ name, val string }{
		{"YC_API_KEY", cfg.APIKey},
		{"YC_FOLDER_ID", cfg.FolderID},
	} {
		if r.val == "" {
			errs = append(errs, fmt.Errorf("%s is required but empty", r.name))
		}
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	} else if u, err := url.Parse(cfg.BaseURL); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("YC_LLM_BASE_URL = %q, want a valid http(s) URL", cfg.BaseURL))
	}
	if v := env("YC_LLM_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("YC_LLM_MAX_TOKENS = %q, want a positive number", v))
		} else {
			cfg.MaxTokens = n
		}
	}
	if len(errs) > 0 {
		return Config{}, fmt.Errorf("aistudio config: %w", errors.Join(errs...))
	}
	return cfg, nil
}

// Client calls AI Studio chat completions. It is safe for concurrent use.
type Client struct {
	cfg     Config
	http    *http.Client
	backoff time.Duration

	mu       sync.Mutex
	noSchema bool // the API rejected structured output
}

var _ providers.Provider = (*Client)(nil)

// New returns a Client.
func New(cfg Config) (*Client, error) {
	if cfg.APIKey == "" || cfg.FolderID == "" || cfg.Model == "" || cfg.BaseURL == "" || cfg.MaxTokens <= 0 {
		return nil, errors.New("aistudio: APIKey, FolderID, Model, BaseURL and MaxTokens are required")
	}
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				TLSClientConfig:     &tls.Config{RootCAs: tlsroot.Pool(), MinVersion: tls.VersionTLS12},
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 4,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 15 * time.Second,
			},
			Timeout: httpTimeout,
		}
	}
	return &Client{cfg: cfg, http: hc, backoff: backoffBase}, nil
}

// APIError is a non-2xx answer from the API.
type APIError struct {
	Status int
	Body   string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("aistudio: status %d: %s", e.Status, e.Body)
}

// transportError is a failure to reach the API or to read its answer, the
// only kind of non-HTTP error that may pass on a retry.
type transportError struct{ err error }

func (e transportError) Error() string { return e.err.Error() }
func (e transportError) Unwrap() error { return e.err }

// Complete sends one chat completion, retrying rate limits, server and
// network errors. When the model cannot answer anyone for a while, the error
// wraps providers.ErrUnavailable.
func (c *Client) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	if req.Source != "" {
		req.User = req.Source + "\n" + req.User
	}
	if len(req.PDFs) > 0 {
		return providers.Response{}, providers.ErrCapability
	}
	resp, err := c.complete(ctx, req)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		// The caller gave up, whatever the last answer was: the provider is
		// not to blame.
		if !errors.Is(err, ctx.Err()) {
			err = fmt.Errorf("aistudio: %w (last error: %w)", ctx.Err(), err)
		}
		return providers.Response{}, err
	case unavailable(err):
		return providers.Response{}, fmt.Errorf("%w: %w", err, providers.ErrUnavailable)
	}
	return resp, err
}

// complete retries one request. If the API rejects structured output with
// 400, the same request goes again with the schema in the system prompt;
// when that works, later calls skip straight to the plain form.
func (c *Client) complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	withSchema := len(req.Schema) > 0 && !c.schemaOff()
	droppedSchema := false
	for attempt := 1; ; attempt++ {
		resp, err := c.call(ctx, req, withSchema)
		if err == nil {
			if droppedSchema {
				c.setSchemaOff()
			}
			return resp, nil
		}

		var apiErr *APIError
		if withSchema && errors.As(err, &apiErr) && apiErr.Status == http.StatusBadRequest {
			slog.Warn("aistudio: structured output rejected, retrying with the schema in the prompt", "body", apiErr.Body)
			withSchema, droppedSchema = false, true
			attempt--
			continue
		}
		if ctx.Err() != nil || !retryable(err) || attempt >= maxAttempts {
			return providers.Response{}, err
		}

		wait := c.backoffFor(attempt)
		slog.Warn("aistudio: request failed, retrying",
			"attempt", attempt, "wait", wait.Round(time.Millisecond), "err", err)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return providers.Response{}, fmt.Errorf("aistudio: %w (last error: %w)", ctx.Err(), err)
		}
	}
}

func (c *Client) schemaOff() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.noSchema
}

func (c *Client) setSchemaOff() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.noSchema = true
}

// message content is a string, or parts when an image goes along.
type message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

type contentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *imageURL `json:"image_url,omitempty"`
}

type imageURL struct {
	URL string `json:"url"`
}

type responseFormat struct {
	Type       string     `json:"type"`
	JSONSchema jsonSchema `json:"json_schema"`
}

type jsonSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
}

type chatRequest struct {
	Model       string    `json:"model"`
	Messages    []message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens"`
	Stream      bool      `json:"stream"`
	// ReasoningEffort none turns thinking off: rewriting a page or writing
	// cards needs none, and thinking costs tokens and time.
	ReasoningEffort string          `json:"reasoning_effort"`
	ResponseFormat  *responseFormat `json:"response_format,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content *string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (c *Client) call(ctx context.Context, req providers.Request, withSchema bool) (result providers.Response, callErr error) {
	system := req.System
	var format *responseFormat
	switch {
	case withSchema:
		format = &responseFormat{Type: "json_schema", JSONSchema: jsonSchema{Name: "answer", Schema: req.Schema}}
	case len(req.Schema) > 0:
		system += prompts.SchemaNote + string(req.Schema)
	}

	var user any = req.User
	if images := providers.RequestImages(req); len(images) > 0 {
		parts := []contentPart{{Type: "text", Text: req.User}}
		for _, img := range images {
			parts = append(parts, contentPart{Type: "image_url", ImageURL: &imageURL{URL: "data:" + img.MimeType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)}})
		}
		user = parts
	}

	effort := "none"
	if req.ReasoningEffort != "" {
		effort = req.ReasoningEffort
	}
	maxTokens := c.cfg.MaxTokens
	if req.MaxTokens > 0 {
		maxTokens = min(maxTokens, req.MaxTokens)
	}
	body, err := json.Marshal(chatRequest{
		Model:           c.modelURI(),
		Messages:        []message{{Role: "system", Content: system}, {Role: "user", Content: user}},
		Temperature:     req.Temperature,
		MaxTokens:       maxTokens,
		ReasoningEffort: effort,
		ResponseFormat:  format,
	})
	if err != nil {
		return providers.Response{}, fmt.Errorf("aistudio: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return providers.Response{}, fmt.Errorf("aistudio: create request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json")
	httpReq.Header.Set("Authorization", "Api-Key "+c.cfg.APIKey)
	httpReq.Header.Set("x-folder-id", c.cfg.FolderID)

	finish, reserveErr := providers.ReserveCall(ctx, 0)
	if reserveErr != nil {
		return providers.Response{}, reserveErr
	}
	defer func() { finish(result.Usage, false) }()
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return providers.Response{}, fmt.Errorf("aistudio: chat request: %w", transportError{err})
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return providers.Response{}, fmt.Errorf("aistudio: read chat response: %w", transportError{err})
	}
	if resp.StatusCode != http.StatusOK {
		return providers.Response{}, &APIError{Status: resp.StatusCode, Body: clip(respBody)}
	}

	var cr chatResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return providers.Response{}, fmt.Errorf("aistudio: decode chat response: %w", err)
	}
	if len(cr.Choices) == 0 {
		return providers.Response{}, fmt.Errorf("aistudio: chat response has no choices: %s", clip(respBody))
	}
	choice := cr.Choices[0]
	switch choice.FinishReason {
	case "content_filter":
		return providers.Response{}, fmt.Errorf("aistudio: %s: %w", c.cfg.Model, providers.ErrRefused)
	case "length":
		// Passed on anyway, marked Truncated: the caller decides what of
		// it to use.
		slog.Warn("aistudio: answer cut short", "model", c.cfg.Model, "finish_reason", choice.FinishReason)
	}
	var content string
	if choice.Message.Content != nil {
		content = *choice.Message.Content
	}
	return providers.Response{
		Content: []byte(content),
		Usage: providers.Usage{
			PromptTokens:     cr.Usage.PromptTokens,
			CompletionTokens: cr.Usage.CompletionTokens,
		},
		Model:     c.cfg.Model,
		Truncated: choice.FinishReason == "length",
	}, nil
}

// modelURI: a bare model name lives in the configured folder.
func (c *Client) modelURI() string {
	if strings.HasPrefix(c.cfg.Model, "gpt://") {
		return c.cfg.Model
	}
	return "gpt://" + c.cfg.FolderID + "/" + c.cfg.Model
}

// retryable: rate limits, server errors and network trouble pass with time.
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

// unavailable: the model will not answer anyone for a while: it keeps failing
// after the retries, or the key, the role or the model is wrong (401, 403,
// 404).
func unavailable(err error) bool {
	if retryable(err) {
		return true
	}
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		return false
	}
	switch apiErr.Status {
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return true
	}
	return false
}

func (c *Client) backoffFor(attempt int) time.Duration {
	d := min(c.backoff<<(attempt-1), backoffMax)
	// ±20% jitter so parallel clients do not retry in lockstep.
	return time.Duration(float64(d) * (0.8 + 0.4*rand.Float64()))
}

func clip(b []byte) string {
	s := strings.TrimSpace(string(b))
	if r := []rune(s); len(r) > logBodyLength {
		return string(r[:logBodyLength]) + "…"
	}
	return s
}
