package http

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/speechkit"
)

// fakeRecognizer hears text in any recording and keeps what it was given.
type fakeRecognizer struct {
	text string
	err  error

	mu    sync.Mutex
	calls [][]byte
}

func (f *fakeRecognizer) Recognize(_ context.Context, pcm []byte) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, pcm)
	return f.text, f.err
}

func (f *fakeRecognizer) called() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.calls)
}

func (s notesServer) speak(auth string, pcm []byte) *httptest.ResponseRecorder {
	s.t.Helper()
	var body bytes.Buffer
	w := multipart.NewWriter(&body)
	part, err := w.CreateFormFile("audio", "answer.pcm")
	if err != nil {
		s.t.Fatal(err)
	}
	if _, err := part.Write(pcm); err != nil {
		s.t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		s.t.Fatal(err)
	}
	r := httptest.NewRequest(http.MethodPost, "/api/speech", &body)
	r.Header.Set("Content-Type", w.FormDataContentType())
	r.Header.Set("Authorization", auth)
	rec := httptest.NewRecorder()
	s.router.ServeHTTP(rec, r)
	return rec
}

func TestRecognizeSpeechReturnsTheText(t *testing.T) {
	users := notesUsers()
	stt := &fakeRecognizer{text: "Первичный ключ"}
	server := newNotesServer(t, NewAPIHandler(nil, nil, nil, users).WithSpeech(stt), users)
	pcm := []byte{1, 0, 2, 0, 3, 0}

	w := server.speak(server.auth, pcm)
	if w.Code != http.StatusOK {
		t.Fatalf("speech: %d %s", w.Code, w.Body.String())
	}
	var got struct{ Text string }
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.Text != "Первичный ключ" {
		t.Fatalf("body %s: %v", w.Body.String(), err)
	}
	if stt.called() != 1 || !bytes.Equal(stt.calls[0], pcm) {
		t.Fatalf("recognizer got %v, want the recording as is", stt.calls)
	}
}

func TestRecognizeSpeechRefusals(t *testing.T) {
	users := notesUsers()
	stt := &fakeRecognizer{text: "да"}
	server := newNotesServer(t, NewAPIHandler(nil, nil, nil, users).WithSpeech(stt), users)

	// In production the unsigned request is the shared account, and every
	// recording costs money.
	if w := server.speak("", []byte{0, 0}); w.Code != http.StatusForbidden {
		t.Fatalf("unsigned: %d %s", w.Code, w.Body.String())
	}
	if w := server.speak(server.auth, nil); w.Code != http.StatusBadRequest {
		t.Fatalf("empty: %d %s", w.Code, w.Body.String())
	}
	// 16-bit samples come in pairs of bytes.
	if w := server.speak(server.auth, []byte{0, 0, 0}); w.Code != http.StatusBadRequest {
		t.Fatalf("odd length: %d %s", w.Code, w.Body.String())
	}
	if w := server.speak(server.auth, make([]byte, speechkit.MaxPCMBytes+2)); w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("over 30 seconds: %d %s", w.Code, w.Body.String())
	}
	if n := stt.called(); n != 0 {
		t.Fatalf("recognizer called %d times on refused recordings", n)
	}
	// Exactly 30 seconds is taken.
	if w := server.speak(server.auth, make([]byte, speechkit.MaxPCMBytes)); w.Code != http.StatusOK {
		t.Fatalf("30 seconds: %d %s", w.Code, w.Body.String())
	}

	off := newNotesServer(t, NewAPIHandler(nil, nil, nil, users), users)
	if w := off.speak(off.auth, []byte{0, 0}); w.Code != http.StatusServiceUnavailable {
		t.Fatalf("without recognition: %d %s", w.Code, w.Body.String())
	}
}

func TestRecognizeSpeechFailureIsInternal(t *testing.T) {
	users := notesUsers()
	stt := &fakeRecognizer{err: errors.New("speechkit: status 401: secret details")}
	server := newNotesServer(t, NewAPIHandler(nil, nil, nil, users).WithSpeech(stt), users)

	w := server.speak(server.auth, []byte{0, 0})
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("failed recognition: %d %s", w.Code, w.Body.String())
	}
	// The service's reply stays in the log.
	if strings.Contains(w.Body.String(), "secret") {
		t.Fatalf("reply shows the service's error: %s", w.Body.String())
	}
}

// A body far over the limit is cut off while it arrives, not held in memory.
func TestRecognizeSpeechStopsReadingPastTheLimit(t *testing.T) {
	users := notesUsers()
	server := newNotesServer(t, NewAPIHandler(nil, nil, nil, users).WithSpeech(&fakeRecognizer{}), users)
	body := &countingReader{r: io.MultiReader(strings.NewReader("--x\r\nContent-Disposition: form-data; name=\"audio\"; filename=\"a\"\r\n\r\n"), strings.NewReader(strings.Repeat("x", 8<<20)))}
	r := httptest.NewRequest(http.MethodPost, "/api/speech", body)
	r.Header.Set("Content-Type", "multipart/form-data; boundary=x")
	r.Header.Set("Authorization", server.auth)
	w := httptest.NewRecorder()
	server.router.ServeHTTP(w, r)
	if w.Code < 400 || body.n > 2<<20 {
		t.Fatalf("huge body: %d, read %d bytes", w.Code, body.n)
	}
}
