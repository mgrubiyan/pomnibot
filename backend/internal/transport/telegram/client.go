// Package telegram implements the standard Telegram Bot API and polling bot.
package telegram

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// MaxFileSize is Telegram's standard Bot API download limit.
const MaxFileSize int64 = 20 * 1024 * 1024

var errFileTooLarge = errors.New("file too large")

// Client keeps token-bearing URLs and API descriptions out of returned errors.
type Client struct {
	token   string
	baseURL string
	http    *http.Client
}

// NewClient creates a client for the public Telegram Bot API.
func NewClient(token string) *Client {
	return &Client{token: token, baseURL: "https://api.telegram.org", http: &http.Client{Timeout: 45 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}

type apiResponse struct {
	OK         bool            `json:"ok"`
	Result     json.RawMessage `json:"result"`
	ErrorCode  int             `json:"error_code"`
	Parameters struct {
		RetryAfter int `json:"retry_after"`
	} `json:"parameters"`
}

func pause(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) call(ctx context.Context, method string, body any, result any) error {
	data, err := json.Marshal(body)
	if err != nil {
		return errors.New("encode Telegram request")
	}
	for {
		request, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/bot"+c.token+"/"+method, bytes.NewReader(data))
		if err != nil {
			return errors.New("create Telegram request")
		}
		request.Header.Set("Content-Type", "application/json")
		response, err := c.http.Do(request)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			return errors.New("telegram request failed")
		}
		var envelope apiResponse
		decodeErr := json.NewDecoder(io.LimitReader(response.Body, 4*1024*1024)).Decode(&envelope)
		_ = response.Body.Close()
		if decodeErr != nil {
			return errors.New("invalid Telegram response")
		}
		if envelope.ErrorCode == 429 || response.StatusCode == 429 {
			delay := max(envelope.Parameters.RetryAfter, 1)
			if err = pause(ctx, time.Duration(delay)*time.Second); err != nil {
				return err
			}
			continue
		}
		if !envelope.OK || response.StatusCode != http.StatusOK {
			return fmt.Errorf("telegram %s failed (status %d, code %d)", method, response.StatusCode, envelope.ErrorCode)
		}
		if result == nil {
			return nil
		}
		if err = json.Unmarshal(envelope.Result, result); err != nil {
			return errors.New("decode Telegram result")
		}
		return nil
	}
}

// User is a Telegram sender.
type User struct {
	ID        int64  `json:"id"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Username  string `json:"username"`
	IsBot     bool   `json:"is_bot"`
}

// File describes an uploaded document or photo rendition.
type File struct {
	ID     string `json:"file_id"`
	Name   string `json:"file_name"`
	Size   int64  `json:"file_size"`
	Width  int64  `json:"width"`
	Height int64  `json:"height"`
}

// Message is the subset of personal messages this bot consumes.
type Message struct {
	ID   int64 `json:"message_id"`
	From *User `json:"from"`
	Chat struct {
		ID   int64  `json:"id"`
		Type string `json:"type"`
	} `json:"chat"`
	Text         string `json:"text"`
	Caption      string `json:"caption"`
	Document     *File  `json:"document"`
	Photos       []File `json:"photo"`
	MediaGroupID string `json:"media_group_id"`
}

// Update is a Telegram long-polling event.
type Update struct {
	ID      int64    `json:"update_id"`
	Message *Message `json:"message"`
}

// GetUpdates confirms earlier updates using the next unprocessed offset.
func (c *Client) GetUpdates(ctx context.Context, offset int64) ([]Update, error) {
	var updates []Update
	err := c.call(ctx, "getUpdates", map[string]any{"offset": offset, "timeout": 30, "allowed_updates": []string{"message"}}, &updates)
	return updates, err
}

// Start validates the bot token and disables webhook delivery for polling.
func (c *Client) Start(ctx context.Context) error {
	if err := c.call(ctx, "getMe", struct{}{}, nil); err != nil {
		return err
	}
	return c.call(ctx, "deleteWebhook", map[string]any{"drop_pending_updates": false}, nil)
}

// Send sends or edits a progress message. appURL adds an inline Mini App button.
func (c *Client) Send(ctx context.Context, chatID, messageID int64, text, appURL string) (int64, error) {
	body := map[string]any{"chat_id": chatID, "text": text}
	if appURL != "" {
		body["reply_markup"] = map[string]any{"inline_keyboard": [][]map[string]any{{{"text": "Открыть Помнибот 📚", "web_app": map[string]string{"url": appURL}}}}}
	}
	method := "sendMessage"
	if messageID != 0 {
		method = "editMessageText"
		body["message_id"] = messageID
	}
	var message struct {
		ID int64 `json:"message_id"`
	}
	err := c.call(ctx, method, body, &message)
	return message.ID, err
}

// Download checks both the API's metadata and the actual response byte count.
func (c *Client) Download(ctx context.Context, fileID string) ([]byte, error) {
	var file struct {
		Path string `json:"file_path"`
		Size int64  `json:"file_size"`
	}
	if err := c.call(ctx, "getFile", map[string]string{"file_id": fileID}, &file); err != nil {
		return nil, err
	}
	if file.Size > MaxFileSize {
		return nil, errFileTooLarge
	}
	// Never allow a file path to change the host or escape the file endpoint.
	if file.Path == "" || strings.HasPrefix(file.Path, "/") || strings.Contains(file.Path, "..") || strings.ContainsAny(file.Path, "?#") {
		return nil, errors.New("invalid Telegram file path")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/file/bot"+c.token+"/"+file.Path, nil)
	if err != nil {
		return nil, errors.New("create Telegram download")
	}
	response, err := c.http.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, errors.New("telegram download failed")
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("telegram download failed")
	}
	if response.ContentLength > MaxFileSize {
		return nil, errFileTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxFileSize+1))
	if err != nil {
		return nil, errors.New("read Telegram download")
	}
	if int64(len(data)) > MaxFileSize {
		return nil, errFileTooLarge
	}
	return data, nil
}

// ValidAppURL ensures Telegram's web_app button receives an HTTPS URL.
func ValidAppURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && u.Scheme == "https" && u.Host != ""
}
