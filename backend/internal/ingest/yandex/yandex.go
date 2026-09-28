// Package yandex is an ingest.OCR on Yandex Vision OCR, plain net/http.
//
// Images and one-page PDFs go to the synchronous recognizeText. A longer PDF
// can only be recognized asynchronously: recognizeTextAsync returns an
// operation, which is polled on the operation service until done, and
// getRecognition then streams the result as JSON Lines, one page per line.
package yandex

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/tlsroot"
)

// Defaults, overridable through Config.
const (
	DefaultBaseURL      = "https://ai.api.cloud.yandex.net"
	DefaultOperationURL = "https://operation.api.cloud.yandex.net"
	DefaultPollInterval = 2 * time.Second
)

const (
	maxAttempts   = 3
	backoffBase   = time.Second
	httpTimeout   = 2 * time.Minute // a safety net; calls are bounded by their context
	maxBodyBytes  = 64 << 20        // a 200-page result is large
	logBodyLength = 500
)

// Languages are the recognition languages: Russian notes with English terms.
var Languages = []string{"ru", "en"}

// Config configures a Client. APIKey is required. FolderID may be empty with
// a service account's API key: the service then works in that account's
// folder.
type Config struct {
	APIKey       string
	FolderID     string
	BaseURL      string
	OperationURL string
	PollInterval time.Duration
	HTTPClient   *http.Client // nil: a client with the system roots plus the Russian Trusted Root CA
}

// ConfigFromEnv reads YC_API_KEY and YC_FOLDER_ID.
func ConfigFromEnv() Config {
	return Config{
		APIKey:   os.Getenv("YC_API_KEY"),
		FolderID: os.Getenv("YC_FOLDER_ID"),
	}
}

// Client calls Yandex Vision OCR. It is safe for concurrent use.
type Client struct {
	cfg     Config
	http    *http.Client
	backoff time.Duration
}

var _ ingest.OCR = (*Client)(nil)

// New returns a Client.
func New(cfg Config) (*Client, error) {
	if cfg.APIKey == "" {
		return nil, errors.New("yandex: YC_API_KEY is required")
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.OperationURL == "" {
		cfg.OperationURL = DefaultOperationURL
	}
	cfg.BaseURL = strings.TrimSuffix(cfg.BaseURL, "/")
	cfg.OperationURL = strings.TrimSuffix(cfg.OperationURL, "/")
	if cfg.PollInterval <= 0 {
		cfg.PollInterval = DefaultPollInterval
	}
	hc := cfg.HTTPClient
	if hc == nil {
		hc = &http.Client{
			Transport: &http.Transport{
				Proxy:               http.ProxyFromEnvironment,
				TLSClientConfig:     &tls.Config{RootCAs: tlsroot.Pool(), MinVersion: tls.VersionTLS12},
				ForceAttemptHTTP2:   true,
				MaxIdleConns:        10,
				IdleConnTimeout:     90 * time.Second,
				TLSHandshakeTimeout: 15 * time.Second,
			},
			Timeout: httpTimeout,
		}
	}
	return &Client{cfg: cfg, http: hc, backoff: backoffBase}, nil
}

// APIError is a non-2xx answer from the service.
type APIError struct {
	Status int
	Body   string
	op     string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("yandex: %s: status %d: %s", e.op, e.Status, e.Body)
}

type recognizeRequest struct {
	MimeType      string   `json:"mimeType"`
	LanguageCodes []string `json:"languageCodes"`
	Model         string   `json:"model"`
	Content       string   `json:"content"`
}

// Recognize sends one file: synchronously for an image or a one-page PDF,
// asynchronously for a longer PDF.
func (c *Client) Recognize(ctx context.Context, req ingest.OCRRequest) ([]ingest.OCRPage, error) {
	body, err := json.Marshal(recognizeRequest{
		MimeType:      req.MimeType,
		LanguageCodes: Languages,
		Model:         req.Model,
		Content:       base64.StdEncoding.EncodeToString(req.Data),
	})
	if err != nil {
		return nil, fmt.Errorf("yandex: marshal request: %w", err)
	}
	if req.Pages <= 1 {
		resp, err := c.do(ctx, http.MethodPost, c.cfg.BaseURL+"/ocr/v1/recognizeText", body, "recognizeText")
		if err != nil {
			return nil, err
		}
		return parseRecognition(resp)
	}

	resp, err := c.do(ctx, http.MethodPost, c.cfg.BaseURL+"/ocr/v1/recognizeTextAsync", body, "recognizeTextAsync")
	if err != nil {
		return nil, err
	}
	var op operation
	if err := json.Unmarshal(resp, &op); err != nil || op.ID == "" {
		return nil, fmt.Errorf("yandex: recognizeTextAsync returned no operation: %s", clip(resp))
	}
	if err := c.wait(ctx, op); err != nil {
		return nil, err
	}
	resp, err = c.do(ctx, http.MethodGet,
		c.cfg.BaseURL+"/ocr/v1/getRecognition?operationId="+url.QueryEscape(op.ID), nil, "getRecognition")
	if err != nil {
		return nil, err
	}
	return parseRecognition(resp)
}

type operation struct {
	ID    string    `json:"id"`
	Done  bool      `json:"done"`
	Error *rpcError `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// wait polls the operation until it is done.
func (c *Client) wait(ctx context.Context, op operation) error {
	for !op.Done {
		select {
		case <-time.After(c.cfg.PollInterval):
		case <-ctx.Done():
			return fmt.Errorf("yandex: waiting for operation %s: %w", op.ID, ctx.Err())
		}
		resp, err := c.do(ctx, http.MethodGet, c.cfg.OperationURL+"/operations/"+url.PathEscape(op.ID), nil, "operation")
		if err != nil {
			return err
		}
		if err := json.Unmarshal(resp, &op); err != nil {
			return fmt.Errorf("yandex: decode operation: %w", err)
		}
	}
	if op.Error != nil && op.Error.Code != 0 {
		return fmt.Errorf("yandex: recognition failed: %d %s", op.Error.Code, op.Error.Message)
	}
	return nil
}

// do sends one request, retrying rate limits and server errors.
func (c *Client) do(ctx context.Context, method, target string, body []byte, op string) ([]byte, error) {
	for attempt := 1; ; attempt++ {
		resp, err := c.send(ctx, method, target, body, op)
		if err == nil {
			return resp, nil
		}
		var apiErr *APIError
		retry := errors.As(err, &apiErr) && (apiErr.Status == http.StatusTooManyRequests || apiErr.Status >= 500)
		if !retry || attempt >= maxAttempts || ctx.Err() != nil {
			return nil, err
		}
		select {
		case <-time.After(c.backoff << (attempt - 1)):
		case <-ctx.Done():
			return nil, err
		}
	}
}

func (c *Client) send(ctx context.Context, method, target string, body []byte, op string) ([]byte, error) {
	var r io.Reader
	if body != nil {
		r = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, r)
	if err != nil {
		return nil, fmt.Errorf("yandex: create request: %w", err)
	}
	req.Header.Set("Authorization", "Api-Key "+c.cfg.APIKey)
	if c.cfg.FolderID != "" {
		req.Header.Set("x-folder-id", c.cfg.FolderID)
	}
	// Notes are the students' personal material: no logging on the service
	// side.
	req.Header.Set("x-data-logging-enabled", "false")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yandex: %s: %w", op, err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("yandex: %s: read response: %w", op, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Body: clip(data), op: op}
	}
	return data, nil
}

// recognition is one page of a result. A streamed response wraps it in
// "result"; page is int64, which JSON carries as a string and omits for page
// zero.
type recognition struct {
	TextAnnotation struct {
		FullText string `json:"fullText"`
		Blocks   []struct {
			Lines []struct {
				Words []struct {
					Confidence *float64 `json:"confidence"`
				} `json:"words"`
			} `json:"lines"`
		} `json:"blocks"`
	} `json:"textAnnotation"`
	Page json.RawMessage `json:"page"`
}

type line struct {
	Result *recognition `json:"result"`
	Error  *rpcError    `json:"error"`
	recognition
}

// parseRecognition reads a result line by line: getRecognition answers with
// JSON Lines, one page per line, which a single json.Unmarshal cannot read.
// Pages are put in order by their number, whatever order the lines came in.
func parseRecognition(data []byte) ([]ingest.OCRPage, error) {
	type numbered struct {
		n    int
		page ingest.OCRPage
	}
	var pages []numbered
	seen := map[int]bool{}

	r := bufio.NewReader(bytes.NewReader(data))
	for {
		raw, err := r.ReadBytes('\n')
		if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 {
			var l line
			if err := json.Unmarshal(trimmed, &l); err != nil {
				return nil, fmt.Errorf("yandex: decode result line: %w", err)
			}
			if l.Error != nil && l.Error.Code != 0 {
				return nil, fmt.Errorf("yandex: recognition failed: %d %s", l.Error.Code, l.Error.Message)
			}
			rec := &l.recognition
			if l.Result != nil {
				rec = l.Result
			}
			n, err := pageNumber(rec.Page)
			if err != nil {
				return nil, err
			}
			if !seen[n] {
				seen[n] = true
				pages = append(pages, numbered{n: n, page: toPage(rec)})
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("yandex: read result: %w", err)
		}
	}
	sort.Slice(pages, func(i, j int) bool { return pages[i].n < pages[j].n })

	out := make([]ingest.OCRPage, len(pages))
	for i, p := range pages {
		out[i] = p.page
	}
	return out, nil
}

func pageNumber(raw json.RawMessage) (int, error) {
	s := strings.Trim(string(raw), `"`)
	if s == "" || s == "null" {
		return 0, nil
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("yandex: bad page number %s", raw)
	}
	return n, nil
}

// toPage takes the full text and, if the service reports word confidences,
// their mean.
func toPage(rec *recognition) ingest.OCRPage {
	p := ingest.OCRPage{Text: rec.TextAnnotation.FullText}
	var sum float64
	var n int
	for _, b := range rec.TextAnnotation.Blocks {
		for _, l := range b.Lines {
			for _, w := range l.Words {
				if w.Confidence != nil {
					sum += *w.Confidence
					n++
				}
			}
		}
	}
	if n > 0 {
		p.Confidence, p.HasConfidence = sum/float64(n), true
	}
	return p
}

func clip(b []byte) string {
	s := strings.TrimSpace(string(b))
	if r := []rune(s); len(r) > logBodyLength {
		return string(r[:logBodyLength]) + "…"
	}
	return s
}
