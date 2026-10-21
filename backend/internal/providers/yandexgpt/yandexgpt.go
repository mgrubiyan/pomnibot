// Package yandexgpt is a providers.Provider for YandexGPT on the Yandex AI
// Studio Text Generation API, on plain net/http.
//
// cardsgen compares it with the app's models on the same notes. It reads
// text only: a request with an image fails at once.
package yandexgpt

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
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
	"github.com/mgrubiyan/pomnibot/backend/internal/tlsroot"
)

// Defaults for the optional settings.
const (
	DefaultModel     = "yandexgpt-5.1"
	DefaultBaseURL   = "https://ai.api.cloud.yandex.net"
	DefaultMaxTokens = 8000
)

const (
	// maxAttempts for 429, 5xx and network errors. Fewer than GigaChat's: this
	// is the backup, and the caller is already late.
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
	// Model is the model name, such as yandexgpt-5.1, or a full URI:
	// gpt://<folder>/yandexgpt-5.1.
	Model      string
	BaseURL    string       // https://ai.api.cloud.yandex.net
	HTTPClient *http.Client // nil: a client with the system roots plus the Russian Trusted Root CA
	// MaxTokens caps the answer. It is set here rather than by the caller:
	// token counts depend on the model's tokenizer and price.
	MaxTokens int
}

func env(key string) string {
	return strings.TrimSpace(os.Getenv(key))
}

// ConfigFromEnv reads YC_API_KEY and YC_FOLDER_ID, shared with the OCR, and
// optionally YC_YANDEXGPT_MODEL, YC_LLM_BASE_URL and YC_LLM_MAX_TOKENS.
func ConfigFromEnv() (Config, error) {
	cfg := Config{
		APIKey:   env("YC_API_KEY"),
		FolderID: env("YC_FOLDER_ID"),
		Model:    env("YC_YANDEXGPT_MODEL"),
		BaseURL:  strings.TrimSuffix(env("YC_LLM_BASE_URL"), "/"),
	}

	var errs []error
	if cfg.Model == "" {
		cfg.Model = DefaultModel
	}
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

	cfg.MaxTokens = DefaultMaxTokens
	if v := env("YC_LLM_MAX_TOKENS"); v != "" {
		if n, err := strconv.Atoi(v); err != nil || n <= 0 {
			errs = append(errs, fmt.Errorf("YC_LLM_MAX_TOKENS = %q, want a positive number", v))
		} else {
			cfg.MaxTokens = n
		}
	}

	if len(errs) > 0 {
		return Config{}, fmt.Errorf("yandexgpt config: %w", errors.Join(errs...))
	}
	return cfg, nil
}

// Client calls the YandexGPT completion. It is safe for concurrent use.
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
		return nil, errors.New("yandexgpt: APIKey, FolderID, Model, BaseURL and MaxTokens are required")
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
	return fmt.Sprintf("yandexgpt: status %d: %s", e.Status, e.Body)
}

// transportError is a failure to reach the API or to read its answer, the
// only kind of non-HTTP error that may pass on a retry.
type transportError struct{ err error }

func (e transportError) Error() string { return e.err.Error() }
func (e transportError) Unwrap() error { return e.err }

// errNoImages: the models read text only.
var errNoImages = errors.New("yandexgpt: images are not supported")

// Complete sends one completion, retrying rate limits, server and network
// errors. When YandexGPT cannot answer anyone for a while, the error wraps
// providers.ErrUnavailable.
func (c *Client) Complete(ctx context.Context, req providers.Request) (providers.Response, error) {
	if req.Image != nil {
		return providers.Response{}, errNoImages
	}
	resp, err := c.complete(ctx, req)
	switch {
	case err == nil:
	case ctx.Err() != nil:
		// The caller gave up, whatever the last answer was: the provider is
		// not to blame.
		if !errors.Is(err, ctx.Err()) {
			err = fmt.Errorf("yandexgpt: %w (last error: %w)", ctx.Err(), err)
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
			slog.Warn("yandexgpt: structured output rejected, retrying with the schema in the prompt", "body", apiErr.Body)
			withSchema, droppedSchema = false, true
			attempt--
			continue
		}
		if ctx.Err() != nil || !retryable(err) || attempt >= maxAttempts {
			return providers.Response{}, err
		}

		wait := c.backoffFor(attempt)
		slog.Warn("yandexgpt: request failed, retrying",
			"attempt", attempt, "wait", wait.Round(time.Millisecond), "err", err)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return providers.Response{}, fmt.Errorf("yandexgpt: %w (last error: %w)", ctx.Err(), err)
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

type message struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

type completionOptions struct {
	Stream      bool    `json:"stream"`
	Temperature float64 `json:"temperature"`
	MaxTokens   string  `json:"maxTokens"` // int64, which the REST API spells as a string
}

type jsonSchema struct {
	Schema json.RawMessage `json:"schema"`
}

type completionRequest struct {
	ModelURI          string            `json:"modelUri"`
	CompletionOptions completionOptions `json:"completionOptions"`
	Messages          []message         `json:"messages"`
	JSONSchema        *jsonSchema       `json:"jsonSchema,omitempty"`
}

type completion struct {
	Alternatives []struct {
		Message message `json:"message"`
		Status  string  `json:"status"`
	} `json:"alternatives"`
	Usage struct {
		InputTextTokens  string `json:"inputTextTokens"`
		CompletionTokens string `json:"completionTokens"`
	} `json:"usage"`
	ModelVersion string `json:"modelVersion"`
}

// completionResponse: the REST API wraps the completion in "result"; the
// reference shows it bare, so both are read.
type completionResponse struct {
	completion
	Result *completion `json:"result"`
}

const schemaPromptNote = "\n\nОтвет — только JSON, соответствующий этой JSON Schema:\n"

func (c *Client) call(ctx context.Context, req providers.Request, withSchema bool) (result providers.Response, callErr error) {
	system := req.System
	var schema *jsonSchema
	switch {
	case withSchema:
		schema = &jsonSchema{Schema: req.Schema}
	case len(req.Schema) > 0:
		system += schemaPromptNote + string(req.Schema)
	}

	body, err := json.Marshal(completionRequest{
		ModelURI: c.modelURI(),
		CompletionOptions: completionOptions{
			Temperature: min(max(req.Temperature, 0), 1),
			MaxTokens:   strconv.Itoa(c.cfg.MaxTokens),
		},
		Messages:   []message{{Role: "system", Text: system}, {Role: "user", Text: req.User}},
		JSONSchema: schema,
	})
	if err != nil {
		return providers.Response{}, fmt.Errorf("yandexgpt: marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+"/foundationModels/v1/completion", bytes.NewReader(body))
	if err != nil {
		return providers.Response{}, fmt.Errorf("yandexgpt: create request: %w", err)
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
		return providers.Response{}, fmt.Errorf("yandexgpt: completion request: %w", transportError{err})
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return providers.Response{}, fmt.Errorf("yandexgpt: read completion response: %w", transportError{err})
	}
	if resp.StatusCode != http.StatusOK {
		return providers.Response{}, &APIError{Status: resp.StatusCode, Body: clip(respBody)}
	}

	var cr completionResponse
	if err := json.Unmarshal(respBody, &cr); err != nil {
		return providers.Response{}, fmt.Errorf("yandexgpt: decode completion response: %w", err)
	}
	out := cr.completion
	if cr.Result != nil {
		out = *cr.Result
	}
	if len(out.Alternatives) == 0 {
		return providers.Response{}, fmt.Errorf("yandexgpt: completion has no alternatives: %s", clip(respBody))
	}
	alt := out.Alternatives[0]
	switch alt.Status {
	case "ALTERNATIVE_STATUS_CONTENT_FILTER":
		return providers.Response{}, fmt.Errorf("yandexgpt: %s: %w", c.cfg.Model, providers.ErrRefused)
	case "ALTERNATIVE_STATUS_TRUNCATED_FINAL":
		// Passed on anyway: the caller sees an unusable answer and handles it
		// like any other.
		slog.Warn("yandexgpt: answer cut short", "model", c.cfg.Model, "status", alt.Status)
	}

	model := c.cfg.Model
	if out.ModelVersion != "" {
		model += ":" + out.ModelVersion
	}
	return providers.Response{
		Content: []byte(alt.Message.Text),
		Usage: providers.Usage{
			PromptTokens:     atoi(out.Usage.InputTextTokens),
			CompletionTokens: atoi(out.Usage.CompletionTokens),
		},
		Model: model,
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

// unavailable: YandexGPT will not answer anyone for a while: it keeps failing
// after the retries, or the key, the role or the model is wrong (401, 403,
// 404). A bad request, a broken answer or the content filter are about one
// request only.
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

func atoi(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

func clip(b []byte) string {
	s := strings.TrimSpace(string(b))
	if r := []rune(s); len(r) > logBodyLength {
		return string(r[:logBodyLength]) + "…"
	}
	return s
}
