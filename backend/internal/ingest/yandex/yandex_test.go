package yandex

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
)

// fakeVision plays recognizeText, recognizeTextAsync, the operation service
// and getRecognition.
type fakeVision struct {
	syncReply   string
	syncStatus  []int // statuses to answer before syncReply, one per call
	opPollsLeft atomic.Int32
	opError     string
	jsonl       string

	mu      sync.Mutex
	reqs    []*http.Request
	bodies  []recognizeRequest
	opPolls int
}

func (f *fakeVision) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	f.mu.Lock()
	f.reqs = append(f.reqs, r)
	if len(body) > 0 {
		var rr recognizeRequest
		_ = json.Unmarshal(body, &rr)
		f.bodies = append(f.bodies, rr)
	}
	f.mu.Unlock()

	switch {
	case r.URL.Path == "/ocr/v1/recognizeText":
		f.mu.Lock()
		var status int
		if len(f.syncStatus) > 0 {
			status, f.syncStatus = f.syncStatus[0], f.syncStatus[1:]
		}
		f.mu.Unlock()
		if status != 0 {
			w.WriteHeader(status)
			return
		}
		_, _ = io.WriteString(w, f.syncReply)
	case r.URL.Path == "/ocr/v1/recognizeTextAsync":
		_, _ = io.WriteString(w, `{"id":"op-1","done":false}`)
	case r.URL.Path == "/operations/op-1":
		f.mu.Lock()
		f.opPolls++
		f.mu.Unlock()
		if f.opPollsLeft.Add(-1) > 0 {
			_, _ = io.WriteString(w, `{"id":"op-1","done":false}`)
			return
		}
		if f.opError != "" {
			_, _ = io.WriteString(w, `{"id":"op-1","done":true,"error":{"code":3,"message":"`+f.opError+`"}}`)
			return
		}
		_, _ = io.WriteString(w, `{"id":"op-1","done":true,"response":{}}`)
	case r.URL.Path == "/ocr/v1/getRecognition" && r.URL.Query().Get("operationId") == "op-1":
		_, _ = io.WriteString(w, f.jsonl)
	default:
		http.NotFound(w, r)
	}
}

func newTestClient(t *testing.T, f *fakeVision) *Client {
	t.Helper()
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c, err := New(Config{
		APIKey:       "key",
		FolderID:     "folder",
		BaseURL:      srv.URL,
		OperationURL: srv.URL,
		PollInterval: time.Millisecond,
		HTTPClient:   srv.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}
	c.backoff = time.Millisecond
	return c
}

func texts(pages []ingest.OCRPage) []string {
	var out []string
	for _, p := range pages {
		out = append(out, p.Text)
	}
	return out
}

func TestRecognizeImage(t *testing.T) {
	f := &fakeVision{syncReply: `{"result":{"textAnnotation":{"fullText":"Митоз — деление"},"page":"0"}}`}
	c := newTestClient(t, f)

	data := []byte("\xff\xd8 not really a jpeg")
	pages, err := c.Recognize(context.Background(), ingest.OCRRequest{Data: data, MimeType: "image/jpeg", Model: "handwritten", Pages: 1})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if got := texts(pages); !slices.Equal(got, []string{"Митоз — деление"}) {
		t.Errorf("pages = %q", got)
	}

	r, body := f.reqs[0], f.bodies[0]
	for header, want := range map[string]string{
		"Authorization":          "Api-Key key",
		"x-folder-id":            "folder",
		"x-data-logging-enabled": "false",
		"Content-Type":           "application/json",
	} {
		if got := r.Header.Get(header); got != want {
			t.Errorf("%s = %q, want %q", header, got, want)
		}
	}
	content, _ := base64.StdEncoding.DecodeString(body.Content)
	if body.MimeType != "image/jpeg" || body.Model != "handwritten" || !slices.Equal(body.LanguageCodes, []string{"ru", "en"}) || string(content) != string(data) {
		t.Errorf("body = %+v", body)
	}
}

func TestRecognizeMultipagePDFAsync(t *testing.T) {
	// Pages come out of order; page zero has no "page" field, as int64 zero
	// is omitted in JSON; there is a blank line.
	f := &fakeVision{jsonl: strings.Join([]string{
		`{"result":{"textAnnotation":{"fullText":"третья"},"page":"2"}}`,
		`{"result":{"textAnnotation":{"fullText":"первая"}}}`,
		``,
		`{"result":{"textAnnotation":{"fullText":"вторая"},"page":1}}`,
	}, "\n") + "\n"}
	f.opPollsLeft.Store(3)
	c := newTestClient(t, f)

	pages, err := c.Recognize(context.Background(), ingest.OCRRequest{Data: []byte("%PDF"), MimeType: "application/pdf", Model: "page", Pages: 3})
	if err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if got := texts(pages); !slices.Equal(got, []string{"первая", "вторая", "третья"}) {
		t.Errorf("pages = %q, want them in page order", got)
	}
	if f.opPolls != 3 {
		t.Errorf("operation polled %d times, want until done", f.opPolls)
	}
	for _, r := range f.reqs {
		if r.URL.Path == "/ocr/v1/recognizeText" {
			t.Error("a multipage PDF went to the synchronous endpoint")
		}
	}
}

func TestRecognizeAsyncOperationError(t *testing.T) {
	f := &fakeVision{opError: "bad pdf"}
	f.opPollsLeft.Store(1)
	c := newTestClient(t, f)
	_, err := c.Recognize(context.Background(), ingest.OCRRequest{Data: []byte("%PDF"), MimeType: "application/pdf", Pages: 2})
	if err == nil || !strings.Contains(err.Error(), "bad pdf") {
		t.Errorf("error = %v, want the operation error", err)
	}
}

func TestRecognizeRetriesRateLimit(t *testing.T) {
	f := &fakeVision{
		syncStatus: []int{http.StatusTooManyRequests, http.StatusServiceUnavailable},
		syncReply:  `{"result":{"textAnnotation":{"fullText":"текст"}}}`,
	}
	c := newTestClient(t, f)
	pages, err := c.Recognize(context.Background(), ingest.OCRRequest{Data: []byte("x"), MimeType: "image/png", Pages: 1})
	if err != nil || len(pages) != 1 {
		t.Fatalf("Recognize() = %v, %v", pages, err)
	}
	if len(f.reqs) != 3 {
		t.Errorf("%d requests, want 2 retries", len(f.reqs))
	}
}

func TestParseRecognition(t *testing.T) {
	t.Run("bare pages and word confidences", func(t *testing.T) {
		data := `{"textAnnotation":{"fullText":"b","blocks":[{"lines":[{"words":[{"confidence":0.5},{"confidence":0.9}]}]}]},"page":"1"}
{"textAnnotation":{"fullText":"a"},"page":"0"}`
		pages, err := parseRecognition([]byte(data))
		if err != nil {
			t.Fatal(err)
		}
		if got := texts(pages); !slices.Equal(got, []string{"a", "b"}) {
			t.Errorf("pages = %q", got)
		}
		if pages[0].HasConfidence || !pages[1].HasConfidence || pages[1].Confidence < 0.69 || pages[1].Confidence > 0.71 {
			t.Errorf("confidence = %+v", pages)
		}
	})
	t.Run("an error line", func(t *testing.T) {
		_, err := parseRecognition([]byte(`{"error":{"code":13,"message":"internal"}}`))
		if err == nil {
			t.Error("error line accepted")
		}
	})
	t.Run("a broken line", func(t *testing.T) {
		if _, err := parseRecognition([]byte("{\"result\":\n")); err == nil {
			t.Error("broken JSON accepted")
		}
	})
}

func TestFolderIsOptional(t *testing.T) {
	f := &fakeVision{syncReply: `{"result":{"textAnnotation":{"fullText":"текст"}}}`}
	c := newTestClient(t, f)
	c.cfg.FolderID = ""
	if _, err := c.Recognize(context.Background(), ingest.OCRRequest{Data: []byte("x"), MimeType: "image/png", Pages: 1}); err != nil {
		t.Fatalf("Recognize() error = %v", err)
	}
	if _, set := f.reqs[0].Header["X-Folder-Id"]; set {
		t.Error("x-folder-id sent without a folder")
	}
}

func TestNewRequiresCredentials(t *testing.T) {
	if _, err := New(Config{FolderID: "folder"}); err == nil {
		t.Error("New() without a key succeeded")
	}
	t.Setenv("YC_API_KEY", "k")
	t.Setenv("YC_FOLDER_ID", "f")
	if cfg := ConfigFromEnv(); cfg.APIKey != "k" || cfg.FolderID != "f" {
		t.Errorf("ConfigFromEnv() = %+v", cfg)
	}
}
