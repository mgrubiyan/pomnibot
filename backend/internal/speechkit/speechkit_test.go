package speechkit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSTT plays stt:recognize, answering statuses before reply.
type fakeSTT struct {
	reply  string
	status []int // statuses to answer before reply, one per call

	mu     sync.Mutex
	reqs   []*http.Request
	bodies [][]byte
}

func (f *fakeSTT) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, r)
	f.bodies = append(f.bodies, body)
	var status int
	if len(f.status) > 0 {
		status, f.status = f.status[0], f.status[1:]
	}
	f.mu.Unlock()
	if r.URL.Path != "/speech/v1/stt:recognize" {
		http.NotFound(w, r)
		return
	}
	if status != 0 {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, `{"error_code":"BAD","error_message":"nope"}`)
		return
	}
	_, _ = io.WriteString(w, f.reply)
}

func newTestClient(t *testing.T, f *fakeSTT, folder string) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c, err := New(Config{APIKey: " key ", FolderID: folder, BaseURL: srv.URL + "/"})
	if err != nil {
		t.Fatal(err)
	}
	c.backoff = time.Millisecond
	return c
}

func TestRecognizeSendsPCM(t *testing.T) {
	f := &fakeSTT{reply: `{"result":"Первичный ключ"}`}
	c := newTestClient(t, f, "folder-1")
	pcm := []byte{1, 0, 2, 0}

	text, err := c.Recognize(context.Background(), pcm)
	if err != nil {
		t.Fatal(err)
	}
	if text != "Первичный ключ" {
		t.Fatalf("text = %q", text)
	}
	r := f.reqs[0]
	if r.Method != http.MethodPost {
		t.Errorf("method = %s", r.Method)
	}
	q := r.URL.Query()
	for k, want := range map[string]string{"lang": "ru-RU", "format": "lpcm", "sampleRateHertz": "16000", "folderId": "folder-1"} {
		if got := q.Get(k); got != want {
			t.Errorf("query %s = %q, want %q", k, got, want)
		}
	}
	if got := r.Header.Get("Authorization"); got != "Api-Key key" {
		t.Errorf("Authorization = %q", got)
	}
	// Students' voices are not left for the service to train on.
	if got := r.Header.Get("x-data-logging-enabled"); got != "false" {
		t.Errorf("x-data-logging-enabled = %q", got)
	}
	if !bytes.Equal(f.bodies[0], pcm) {
		t.Errorf("body = %v, want the PCM as is", f.bodies[0])
	}
}

func TestRecognizeWithoutFolder(t *testing.T) {
	f := &fakeSTT{reply: `{"result":""}`}
	c := newTestClient(t, f, "")

	text, err := c.Recognize(context.Background(), []byte{0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if text != "" {
		t.Fatalf("text = %q, want empty for silence", text)
	}
	if f.reqs[0].URL.Query().Has("folderId") {
		t.Error("folderId sent without a folder")
	}
}

func TestRecognizeRetriesRateLimitAndServerErrors(t *testing.T) {
	f := &fakeSTT{reply: `{"result":"да"}`, status: []int{http.StatusTooManyRequests, http.StatusBadGateway}}
	c := newTestClient(t, f, "")

	text, err := c.Recognize(context.Background(), []byte{0, 0})
	if err != nil {
		t.Fatal(err)
	}
	if text != "да" || len(f.reqs) != 3 {
		t.Fatalf("text = %q after %d calls, want да after 3", text, len(f.reqs))
	}
}

func TestRecognizeDoesNotRetryClientErrors(t *testing.T) {
	f := &fakeSTT{status: []int{http.StatusUnauthorized, http.StatusUnauthorized}}
	c := newTestClient(t, f, "")

	_, err := c.Recognize(context.Background(), []byte{0, 0})
	var apiErr *APIError
	if !errors.As(err, &apiErr) || apiErr.Status != http.StatusUnauthorized {
		t.Fatalf("err = %v, want APIError 401", err)
	}
	// The status and the service's words make a wrong key plain in the log.
	if !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "nope") {
		t.Errorf("err = %q", err)
	}
	if len(f.reqs) != 1 {
		t.Errorf("%d calls, want 1", len(f.reqs))
	}
}

func TestRecognizeRejectsBadReply(t *testing.T) {
	f := &fakeSTT{reply: `not json`}
	c := newTestClient(t, f, "")

	if _, err := c.Recognize(context.Background(), []byte{0, 0}); err == nil {
		t.Fatal("no error on a reply that is not JSON")
	}
}

func TestNewRequiresKey(t *testing.T) {
	if _, err := New(Config{BaseURL: "https://example.test"}); err == nil {
		t.Fatal("no error without a key")
	}
}

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("YC_API_KEY", " key\n")
	t.Setenv("YC_FOLDER_ID", "folder")
	t.Setenv("YC_STT_BASE_URL", "")

	cfg, ok := ConfigFromEnv()
	if !ok {
		t.Fatal("off with a key")
	}
	if cfg.APIKey != "key" || cfg.FolderID != "folder" || cfg.BaseURL != DefaultBaseURL {
		t.Fatalf("cfg = %+v", cfg)
	}

	t.Setenv("YC_STT_BASE_URL", "https://stt.example.test")
	if cfg, _ := ConfigFromEnv(); cfg.BaseURL != "https://stt.example.test" {
		t.Fatalf("BaseURL = %q, want the override", cfg.BaseURL)
	}

	t.Setenv("YC_API_KEY", "")
	if _, ok := ConfigFromEnv(); ok {
		t.Fatal("on without a key")
	}
}
