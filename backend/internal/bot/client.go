// Package bot implements MAX Bot API client and bot event loop.
package bot

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

//go:embed certs/rootca.pem
var rootCAPEM []byte

const defaultAPIBaseURL = "https://platform-api2.max.ru"

// User represents a MAX user or bot profile according to MAX Bot API sender schema.
type User struct {
	UserID    int64   `json:"user_id"`
	FirstName string  `json:"first_name"`
	LastName  *string `json:"last_name,omitempty"`
	Username  *string `json:"username,omitempty"`
	IsBot     bool    `json:"is_bot"`
	Name      string  `json:"name,omitempty"`
}

// UpdateResponse represents response from GET /updates.
type UpdateResponse struct {
	Updates []Update `json:"updates"`
	Marker  *int64   `json:"marker"`
}

// Update represents a single update event from MAX.
type Update struct {
	UpdateType string   `json:"update_type"`
	Timestamp  int64    `json:"timestamp"`
	ChatID     int64    `json:"chat_id,omitempty"`
	User       *User    `json:"user,omitempty"`
	Message    *Message `json:"message,omitempty"`
}

// Message represents incoming message.
type Message struct {
	Recipient   MessageRecipient `json:"recipient"`
	Sender      User             `json:"sender"`
	Body        MessageBody      `json:"body"`
	Attachments []Attachment     `json:"attachments,omitempty"`
}

// MessageRecipient describes who received the message.
type MessageRecipient struct {
	ChatID   int64  `json:"chat_id"`
	ChatType string `json:"chat_type"`
	UserID   int64  `json:"user_id"`
}

// MessageBody contains the message content.
type MessageBody struct {
	Mid         string       `json:"mid"`
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// SendMessageRequest contains payload for POST /messages.
type SendMessageRequest struct {
	Text        string       `json:"text"`
	Attachments []Attachment `json:"attachments,omitempty"`
}

// Attachment represents attachment (e.g. inline keyboard).
type Attachment struct {
	Type    string                 `json:"type"`
	Payload map[string]interface{} `json:"payload"`
}

// SubscriptionsResponse represents response from GET /subscriptions.
type SubscriptionsResponse struct {
	Subscriptions []Subscription `json:"subscriptions"`
}

// Subscription represents an active webhook subscription.
type Subscription struct {
	URL string `json:"url"`
}

// Client is a MAX Bot API client.
type Client struct {
	token      string
	baseURL    string
	httpClient *http.Client
}

// NewClient creates a new MAX Bot API client with custom TLS config for Russian Trusted Root CA.
func NewClient(token string, baseURL string) (*Client, error) {
	if baseURL == "" {
		baseURL = defaultAPIBaseURL
	}
	baseURL = strings.TrimSuffix(baseURL, "/")

	// Create root CA pool including system CAs and bundled Russian Root CA
	certPool, err := x509.SystemCertPool()
	if err != nil || certPool == nil {
		certPool = x509.NewCertPool()
	}

	if len(rootCAPEM) > 0 {
		certPool.AppendCertsFromPEM(rootCAPEM)
	}

	insecureSkipVerify := os.Getenv("MAX_INSECURE_SKIP_VERIFY") == "true"

	tlsConfig := &tls.Config{
		RootCAs:            certPool,
		InsecureSkipVerify: insecureSkipVerify, //nolint:gosec
	}

	transport := &http.Transport{
		TLSClientConfig:     tlsConfig,
		MaxIdleConns:        100,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}

	httpClient := &http.Client{
		Transport: transport,
		Timeout:   60 * time.Second,
	}

	return &Client{
		token:      token,
		baseURL:    baseURL,
		httpClient: httpClient,
	}, nil
}

func (c *Client) doRequest(ctx context.Context, method, endpoint string, body any, result any) error {
	var bodyReader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("marshal request body: %w", err)
		}
		bodyReader = bytes.NewReader(data)
	}

	reqURL := c.baseURL + endpoint
	req, err := http.NewRequestWithContext(ctx, method, reqURL, bodyReader)
	if err != nil {
		return fmt.Errorf("create request: %w", err)
	}

	req.Header.Set("Authorization", c.token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return fmt.Errorf("do request %s %s: %w", method, reqURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("api error %s %s status %d: %s", method, endpoint, resp.StatusCode, string(respBytes))
	}

	if result != nil && len(respBytes) > 0 {
		if err := json.Unmarshal(respBytes, result); err != nil {
			return fmt.Errorf("unmarshal response: %w", err)
		}
	}

	return nil
}

// GetMe returns the bot's own profile.
func (c *Client) GetMe(ctx context.Context) (*User, error) {
	var user User
	if err := c.doRequest(ctx, http.MethodGet, "/me", nil, &user); err != nil {
		return nil, err
	}
	return &user, nil
}

// GetUpdates polls for updates using long polling.
func (c *Client) GetUpdates(ctx context.Context, marker *int64, timeout int) (*UpdateResponse, error) {
	params := url.Values{}
	if timeout > 0 {
		params.Set("timeout", strconv.Itoa(timeout))
	}
	if marker != nil {
		params.Set("marker", strconv.FormatInt(*marker, 10))
	}

	endpoint := "/updates"
	if len(params) > 0 {
		endpoint += "?" + params.Encode()
	}

	var resp UpdateResponse
	if err := c.doRequest(ctx, http.MethodGet, endpoint, nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// SendMessage sends a message to chatID or userID.
func (c *Client) SendMessage(ctx context.Context, chatID int64, userID int64, msg SendMessageRequest) error {
	params := url.Values{}
	if chatID != 0 {
		params.Set("chat_id", strconv.FormatInt(chatID, 10))
	} else if userID != 0 {
		params.Set("user_id", strconv.FormatInt(userID, 10))
	} else {
		return fmt.Errorf("either chatID or userID must be provided")
	}

	endpoint := "/messages?" + params.Encode()
	return c.doRequest(ctx, http.MethodPost, endpoint, msg, nil)
}

// GetSubscriptions returns all active webhook subscriptions.
func (c *Client) GetSubscriptions(ctx context.Context) (*SubscriptionsResponse, error) {
	var resp SubscriptionsResponse
	if err := c.doRequest(ctx, http.MethodGet, "/subscriptions", nil, &resp); err != nil {
		return nil, err
	}
	return &resp, nil
}

// DeleteSubscription deletes an active webhook subscription.
func (c *Client) DeleteSubscription(ctx context.Context, subURL string) error {
	endpoint := "/subscriptions?url=" + url.QueryEscape(subURL)
	return c.doRequest(ctx, http.MethodDelete, endpoint, nil, nil)
}

// CleanUpSubscriptions removes any active webhook subscriptions to enable long polling.
func (c *Client) CleanUpSubscriptions(ctx context.Context) error {
	subs, err := c.GetSubscriptions(ctx)
	if err != nil {
		return err
	}
	for _, sub := range subs.Subscriptions {
		slog.Info("deleting existing subscription to allow long polling", "url", sub.URL)
		if err := c.DeleteSubscription(ctx, sub.URL); err != nil {
			slog.Warn("failed to delete subscription", "url", sub.URL, "error", err)
		}
	}
	return nil
}
