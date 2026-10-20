package gigachat

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/providers"
)

// fileDeleteTimeout bounds the cleanup after a request, which runs even when
// the request's own context is done.
const fileDeleteTimeout = 30 * time.Second

// upload puts an image into the account's file storage for one request: the
// chat API takes images only as ids of uploaded files.
func (c *Client) upload(ctx context.Context, img *providers.Image) (string, error) {
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	if err := w.WriteField("purpose", "general"); err != nil {
		return "", fmt.Errorf("gigachat: build upload: %w", err)
	}
	header := textproto.MIMEHeader{}
	header.Set("Content-Disposition", `form-data; name="file"; filename="`+imageName(img.MimeType)+`"`)
	header.Set("Content-Type", img.MimeType)
	part, err := w.CreatePart(header)
	if err != nil {
		return "", fmt.Errorf("gigachat: build upload: %w", err)
	}
	if _, err := part.Write(img.Data); err != nil {
		return "", fmt.Errorf("gigachat: build upload: %w", err)
	}
	if err := w.Close(); err != nil {
		return "", fmt.Errorf("gigachat: build upload: %w", err)
	}

	var out struct {
		ID string `json:"id"`
	}
	if err := c.fileRequest(ctx, "/files", w.FormDataContentType(), &body, "upload file", &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("gigachat: upload file: no id in the answer")
	}
	return out.ID, nil
}

// deleteFile removes an uploaded file once its request is done: notes of
// students are not kept in the storage. A failure is only logged.
func (c *Client) deleteFile(id string) {
	ctx, cancel := context.WithTimeout(context.Background(), fileDeleteTimeout)
	defer cancel()
	if err := c.fileRequest(ctx, "/files/"+id+"/delete", "", nil, "delete file", nil); err != nil {
		slog.Warn("gigachat: uploaded file not deleted", "file", id, "err", err)
	}
}

func (c *Client) fileRequest(ctx context.Context, path, contentType string, body io.Reader, op string, out any) error {
	token, err := c.tokens.Token(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.BaseURL+path, body)
	if err != nil {
		return fmt.Errorf("gigachat: create %s request: %w", op, err)
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("gigachat: %s: %w", op, transportError{err})
	}
	defer func() { _ = resp.Body.Close() }()
	respBody, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return fmt.Errorf("gigachat: read %s response: %w", op, transportError{err})
	}
	if resp.StatusCode != http.StatusOK {
		return &APIError{Status: resp.StatusCode, Body: clip(respBody), RetryAfter: retryAfter(resp.Header), op: op}
	}
	if out == nil {
		return nil
	}
	if err := json.Unmarshal(respBody, out); err != nil {
		return fmt.Errorf("gigachat: decode %s response: %w", op, err)
	}
	return nil
}

func imageName(mime string) string {
	if mime == "image/png" {
		return "page.png"
	}
	return "page.jpg"
}
