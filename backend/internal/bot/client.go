// Package bot implements MAX Bot API client and bot event loop.
package bot

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/tlsroot"
)

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

// Attachment represents attachment (e.g. inline keyboard, file, image).
type Attachment struct {
	Type     string                 `json:"type"`
	Filename string                 `json:"filename,omitempty"`
	Name     string                 `json:"name,omitempty"`
	Title    string                 `json:"title,omitempty"`
	URL      string                 `json:"url,omitempty"`
	Token    string                 `json:"token,omitempty"`
	Size     *int64                 `json:"size,omitempty"`
	Payload  map[string]interface{} `json:"payload,omitempty"`
}

// DownloadedFile represents downloaded file contents and metadata.
type DownloadedFile struct {
	Data        []byte
	Filename    string
	ContentType string
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

	// Root CA pool including system CAs and bundled Russian Root CA
	certPool := tlsroot.Pool()

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

// SendMessageResponse represents response payload from POST /messages.
type SendMessageResponse struct {
	Message *Message     `json:"message,omitempty"`
	Body    *MessageBody `json:"body,omitempty"`
}

// SendMessage sends a message to chatID or userID and returns the created message ID (mid) if available.
func (c *Client) SendMessage(ctx context.Context, chatID int64, userID int64, msg SendMessageRequest) (string, error) {
	params := url.Values{}
	if chatID != 0 {
		params.Set("chat_id", strconv.FormatInt(chatID, 10))
	} else if userID != 0 {
		params.Set("user_id", strconv.FormatInt(userID, 10))
	} else {
		return "", fmt.Errorf("either chatID or userID must be provided")
	}

	endpoint := "/messages?" + params.Encode()
	var resp SendMessageResponse
	if err := c.doRequest(ctx, http.MethodPost, endpoint, msg, &resp); err != nil {
		return "", err
	}
	if resp.Message != nil && resp.Message.Body.Mid != "" {
		return resp.Message.Body.Mid, nil
	}
	if resp.Body != nil && resp.Body.Mid != "" {
		return resp.Body.Mid, nil
	}
	return "", nil
}

// EditMessage edits an existing message by its messageID (mid).
func (c *Client) EditMessage(ctx context.Context, messageID string, msg SendMessageRequest) error {
	if messageID == "" {
		return fmt.Errorf("messageID is required")
	}
	endpoint := "/messages?message_id=" + url.QueryEscape(messageID)
	return c.doRequest(ctx, http.MethodPut, endpoint, msg, nil)
}

// SendAction notifies users in chat of bot activity (e.g. "typing_on").
func (c *Client) SendAction(ctx context.Context, chatID int64, action string) error {
	if chatID == 0 {
		return nil
	}
	body := map[string]string{"action": action}
	endpoint := fmt.Sprintf("/chats/%d/actions", chatID)
	return c.doRequest(ctx, http.MethodPost, endpoint, body, nil)
}

var percentHexRegex = regexp.MustCompile(`%[0-9a-fA-F]{2}`)

// cleanFilename cleans, unescapes, and strips path components from a filename.
func cleanFilename(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}

	// Repeatedly URL unescape if percent-encoded tokens are present (up to 3 iterations)
	for i := 0; i < 3 && percentHexRegex.MatchString(name); i++ {
		if unescaped, err := url.QueryUnescape(name); err == nil && unescaped != name {
			name = unescaped
		} else {
			break
		}
	}

	// Strip surrounding quotes if present
	name = strings.Trim(name, "\"'")

	// Normalize both Windows and Unix path separators and strip directory components
	name = path.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" {
		return ""
	}
	return strings.TrimSpace(name)
}

// DownloadFile downloads file bytes and metadata from a given URL.
func (c *Client) DownloadFile(ctx context.Context, fileURL string) (*DownloadedFile, error) {
	if fileURL == "" {
		return nil, fmt.Errorf("file URL is empty")
	}
	if !strings.HasPrefix(fileURL, "http://") && !strings.HasPrefix(fileURL, "https://") {
		fileURL = c.baseURL + "/" + strings.TrimPrefix(fileURL, "/")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fileURL, nil)
	if err != nil {
		return nil, fmt.Errorf("create download request: %w", err)
	}

	if strings.Contains(fileURL, "max.ru") {
		req.Header.Set("Authorization", c.token)
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("download file %s: %w", fileURL, err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read file body: %w", err)
	}

	filename := ""
	if cd := resp.Header.Get("Content-Disposition"); cd != "" {
		if _, params, err := mime.ParseMediaType(cd); err == nil {
			if fn, ok := params["filename*"]; ok && strings.TrimSpace(fn) != "" {
				parts := strings.SplitN(fn, "''", 2)
				if len(parts) == 2 {
					filename = parts[1]
				} else {
					filename = fn
				}
			} else if fn, ok := params["filename"]; ok && strings.TrimSpace(fn) != "" {
				filename = fn
			}
		}
	}
	if filename == "" {
		if u, err := url.Parse(fileURL); err == nil {
			base := path.Base(u.Path)
			if base != "" && base != "." && base != "/" {
				filename = base
			}
		}
	}

	return &DownloadedFile{
		Data:        data,
		Filename:    cleanFilename(filename),
		ContentType: resp.Header.Get("Content-Type"),
	}, nil
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
