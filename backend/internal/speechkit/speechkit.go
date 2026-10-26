// Package speechkit turns a short recording into text on Yandex SpeechKit's
// synchronous recognition, plain net/http.
//
// The service takes up to 30 seconds and 1 MB of audio in one request: mono
// 16-bit PCM at 16 kHz fits both, and a browser can make it without a codec.
package speechkit

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/tlsroot"
)

// DefaultBaseURL is SpeechKit's public endpoint.
const DefaultBaseURL = "https://stt.api.cloud.yandex.net"

// SampleRate is the rate of the PCM the client sends.
const SampleRate = 16000

// MaxPCMBytes is 30 seconds of the PCM, the most one request may carry.
const MaxPCMBytes = 30 * SampleRate * 2

const (
	maxAttempts   = 3
	backoffBase   = 300 * time.Millisecond
	httpTimeout   = 20 * time.Second // a safety net; calls are bounded by their context
	maxBodyBytes  = 64 << 10
	logBodyLength = 500
)

// Config configures a Client. APIKey is required. FolderID may be empty with
// a service account's API key: the service then works in that account's
// folder.
type Config struct {
	APIKey     string
	FolderID   string
	BaseURL    string       // empty: DefaultBaseURL
	HTTPClient *http.Client // nil: a client with the system roots plus the Russian Trusted Root CA
}

// Client calls SpeechKit. It is safe for concurrent use.
type Client struct {
	cfg     Config
	http    *http.Client
	backoff time.Duration
}

// ConfigFromEnv reads the key and folder that Vision OCR uses; the endpoint
// may be overridden with YC_STT_BASE_URL. Without a key recognition is off.
func ConfigFromEnv() (Config, bool) {
	cfg := Config{
		APIKey:   strings.TrimSpace(os.Getenv("YC_API_KEY")),
		FolderID: strings.TrimSpace(os.Getenv("YC_FOLDER_ID")),
		BaseURL:  strings.TrimSpace(os.Getenv("YC_STT_BASE_URL")),
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	return cfg, cfg.APIKey != ""
}

// New returns a Client.
func New(cfg Config) (*Client, error) {
	cfg.APIKey = strings.TrimSpace(cfg.APIKey)
	cfg.FolderID = strings.TrimSpace(cfg.FolderID)
	cfg.BaseURL = strings.TrimSuffix(strings.TrimSpace(cfg.BaseURL), "/")
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultBaseURL
	}
	if cfg.APIKey == "" {
		return nil, errors.New("speechkit: YC_API_KEY is required but empty")
	}
	if p, err := url.Parse(cfg.BaseURL); err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return nil, fmt.Errorf("speechkit: base URL %q, want a valid http(s) URL", cfg.BaseURL)
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
				TLSHandshakeTimeout: 10 * time.Second,
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
}

func (e *APIError) Error() string {
	return fmt.Sprintf("speechkit: stt:recognize: status %d: %s", e.Status, e.Body)
}

type recognizeResponse struct {
	Result *string `json:"result"`
}

// Recognize returns the Russian words heard in pcm, mono 16-bit
// little-endian PCM at SampleRate; silence gives "".
func (c *Client) Recognize(ctx context.Context, pcm []byte) (string, error) {
	q := url.Values{
		"lang":            {"ru-RU"},
		"format":          {"lpcm"},
		"sampleRateHertz": {fmt.Sprint(SampleRate)},
	}
	if c.cfg.FolderID != "" {
		q.Set("folderId", c.cfg.FolderID)
	}
	target := c.cfg.BaseURL + "/speech/v1/stt:recognize?" + q.Encode()

	for attempt := 1; ; attempt++ {
		data, err := c.send(ctx, target, pcm)
		if err == nil {
			var resp recognizeResponse
			if err := json.Unmarshal(data, &resp); err != nil || resp.Result == nil {
				return "", fmt.Errorf("speechkit: unexpected reply: %s", clip(data))
			}
			return strings.TrimSpace(*resp.Result), nil
		}
		var apiErr *APIError
		retry := errors.As(err, &apiErr) && (apiErr.Status == http.StatusTooManyRequests || apiErr.Status >= 500)
		if !retry || attempt >= maxAttempts || ctx.Err() != nil {
			return "", err
		}
		select {
		case <-time.After(c.backoff << (attempt - 1)):
		case <-ctx.Done():
			return "", err
		}
	}
}

func (c *Client) send(ctx context.Context, target string, pcm []byte) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(pcm))
	if err != nil {
		return nil, fmt.Errorf("speechkit: create request: %w", err)
	}
	req.Header.Set("Authorization", "Api-Key "+c.cfg.APIKey)
	// Students' voices: no logging on the service side.
	req.Header.Set("x-data-logging-enabled", "false")
	req.Header.Set("Content-Type", "application/octet-stream")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("speechkit: stt:recognize: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return nil, fmt.Errorf("speechkit: read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, &APIError{Status: resp.StatusCode, Body: clip(data)}
	}
	return data, nil
}

func clip(b []byte) string {
	s := strings.TrimSpace(string(b))
	if r := []rune(s); len(r) > logBodyLength {
		return string(r[:logBodyLength]) + "…"
	}
	return s
}
