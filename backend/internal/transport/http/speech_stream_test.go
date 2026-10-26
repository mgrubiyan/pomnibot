package http

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/mgrubiyan/pomnibot/backend/internal/speechkit"
)

// fakeStreamer hears «слово N» in the N-th piece of audio and «Готово» at
// the end of the recording.
type fakeStreamer struct {
	mu       sync.Mutex
	sessions []*fakeSession
	opened   chan *fakeSession
}

func newFakeStreamer() *fakeStreamer { return &fakeStreamer{opened: make(chan *fakeSession, 4)} }

func (f *fakeStreamer) Open(ctx context.Context) (SpeechSession, error) {
	s := &fakeSession{ctx: ctx, updates: make(chan string, 64)}
	f.mu.Lock()
	f.sessions = append(f.sessions, s)
	f.mu.Unlock()
	f.opened <- s
	return s, nil
}

func (f *fakeStreamer) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.sessions)
}

type fakeSession struct {
	ctx     context.Context
	updates chan string

	mu              sync.Mutex
	audio           bytes.Buffer
	pieces          int
	closed          bool
	sentAfterClosed bool
}

func (s *fakeSession) Send(pcm []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		s.sentAfterClosed = true
		return nil
	}
	s.audio.Write(pcm)
	s.pieces++
	s.updates <- fmt.Sprintf("слово %d", s.pieces)
	return nil
}

func (s *fakeSession) CloseSend() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.updates <- "Готово"
		close(s.updates)
	}
	return nil
}

func (s *fakeSession) Next() (string, error) {
	select {
	case text, ok := <-s.updates:
		if !ok {
			return "", io.EOF
		}
		return text, nil
	case <-s.ctx.Done():
		return "", s.ctx.Err()
	}
}

type streamReply struct {
	Text  string `json:"text"`
	Done  bool   `json:"done"`
	Error string `json:"error"`
}

func dialSpeech(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, resp, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(url, "http")+"/api/speech/stream", nil)
	if err != nil {
		status := 0
		if resp != nil {
			status = resp.StatusCode
		}
		t.Fatalf("dial: %v (status %d)", err, status)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	return conn
}

func sendJSON(t *testing.T, conn *websocket.Conn, v any) {
	t.Helper()
	data, _ := json.Marshal(v)
	if err := conn.Write(context.Background(), websocket.MessageText, data); err != nil {
		t.Fatal(err)
	}
}

func readReply(t *testing.T, conn *websocket.Conn) streamReply {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	var r streamReply
	if err := json.Unmarshal(data, &r); err != nil {
		t.Fatalf("reply %q: %v", data, err)
	}
	return r
}

func speechStreamServer(t *testing.T, handler *APIHandler) (*httptest.Server, string) {
	t.Helper()
	users := notesUsers()
	ns := newNotesServer(t, handler, users)
	server := httptest.NewServer(ns.router)
	t.Cleanup(server.Close)
	return server, ns.auth
}

func TestSpeechStreamTellsTheTextAsAudioArrives(t *testing.T) {
	streamer := newFakeStreamer()
	server, auth := speechStreamServer(t, NewAPIHandler(nil, nil, nil, notesUsers()).WithSpeechStream(streamer))
	conn := dialSpeech(t, server.URL)

	sendJSON(t, conn, map[string]string{"authorization": auth})
	for i, piece := range [][]byte{{1, 0}, {2, 0, 3, 0}} {
		if err := conn.Write(context.Background(), websocket.MessageBinary, piece); err != nil {
			t.Fatal(err)
		}
		if r := readReply(t, conn); r.Text != fmt.Sprintf("слово %d", i+1) || r.Done {
			t.Fatalf("after piece %d: %+v", i+1, r)
		}
	}
	sendJSON(t, conn, map[string]string{"type": "end"})
	if r := readReply(t, conn); r.Text != "Готово" || r.Done {
		t.Fatalf("final text: %+v", r)
	}
	if r := readReply(t, conn); r.Text != "Готово" || !r.Done {
		t.Fatalf("end: %+v", r)
	}
	s := <-streamer.opened
	s.mu.Lock()
	defer s.mu.Unlock()
	if !bytes.Equal(s.audio.Bytes(), []byte{1, 0, 2, 0, 3, 0}) {
		t.Fatalf("audio = %v", s.audio.Bytes())
	}
}

func TestSpeechStreamRefusals(t *testing.T) {
	t.Run("off", func(t *testing.T) {
		server, _ := speechStreamServer(t, NewAPIHandler(nil, nil, nil, notesUsers()))
		_, resp, err := websocket.Dial(context.Background(), "ws"+strings.TrimPrefix(server.URL, "http")+"/api/speech/stream", nil)
		if err == nil || resp == nil || resp.StatusCode != http.StatusServiceUnavailable {
			t.Fatalf("dial without recognition: %v %v", err, resp)
		}
	})

	cases := []struct {
		name, auth, want string
	}{
		// In production the unsigned request is the shared account, and every
		// session costs money.
		{"unsigned", "", "forbidden"},
		{"expired", "max auth_date=1&hash=00", "sign_in_expired"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			streamer := newFakeStreamer()
			server, _ := speechStreamServer(t, NewAPIHandler(nil, nil, nil, notesUsers()).WithSpeechStream(streamer))
			conn := dialSpeech(t, server.URL)
			sendJSON(t, conn, map[string]string{"authorization": c.auth})
			if r := readReply(t, conn); r.Error != c.want {
				t.Fatalf("reply %+v, want error %s", r, c.want)
			}
			if _, _, err := conn.Read(context.Background()); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
				t.Fatalf("close = %v, want policy violation", err)
			}
			if n := streamer.count(); n != 0 {
				t.Fatalf("%d sessions opened for a refused reader", n)
			}
		})
	}

	t.Run("audio before signing in", func(t *testing.T) {
		streamer := newFakeStreamer()
		server, _ := speechStreamServer(t, NewAPIHandler(nil, nil, nil, notesUsers()).WithSpeechStream(streamer))
		conn := dialSpeech(t, server.URL)
		if err := conn.Write(context.Background(), websocket.MessageBinary, []byte{0, 0}); err != nil {
			t.Fatal(err)
		}
		if _, _, err := conn.Read(context.Background()); websocket.CloseStatus(err) != websocket.StatusPolicyViolation {
			t.Fatalf("close = %v, want policy violation", err)
		}
		if n := streamer.count(); n != 0 {
			t.Fatalf("%d sessions opened", n)
		}
	})
}

func TestSpeechStreamWaitsForSignInOnlyBriefly(t *testing.T) {
	handler := NewAPIHandler(nil, nil, nil, notesUsers()).WithSpeechStream(newFakeStreamer())
	handler.speechAuthTime = 50 * time.Millisecond
	server, _ := speechStreamServer(t, handler)
	conn := dialSpeech(t, server.URL)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, _, err := conn.Read(ctx); err == nil || ctx.Err() != nil {
		t.Fatalf("silent client kept: %v", err)
	}
}

// A recording stops at 30 seconds, as much as the synchronous endpoint
// takes: the session is told the recording is over and gets no more.
func TestSpeechStreamStopsAtThirtySeconds(t *testing.T) {
	streamer := newFakeStreamer()
	server, auth := speechStreamServer(t, NewAPIHandler(nil, nil, nil, notesUsers()).WithSpeechStream(streamer))
	conn := dialSpeech(t, server.URL)
	sendJSON(t, conn, map[string]string{"authorization": auth})
	// Pieces as long as the mini app's would not end on the limit exactly.
	piece := make([]byte, 29000)
	for range speechkit.MaxPCMBytes/len(piece) + 2 {
		if err := conn.Write(context.Background(), websocket.MessageBinary, piece); err != nil {
			t.Fatal(err)
		}
	}
	var last streamReply
	for !last.Done {
		last = readReply(t, conn)
	}
	s := <-streamer.opened
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.audio.Len() != speechkit.MaxPCMBytes || s.sentAfterClosed {
		t.Fatalf("session got %d bytes (after close: %v), want %d", s.audio.Len(), s.sentAfterClosed, speechkit.MaxPCMBytes)
	}
}

// A reader who leaves mid-sentence stops the paid session.
func TestSpeechStreamEndsWithTheReader(t *testing.T) {
	streamer := newFakeStreamer()
	server, auth := speechStreamServer(t, NewAPIHandler(nil, nil, nil, notesUsers()).WithSpeechStream(streamer))
	conn := dialSpeech(t, server.URL)
	sendJSON(t, conn, map[string]string{"authorization": auth})
	if err := conn.Write(context.Background(), websocket.MessageBinary, []byte{0, 0}); err != nil {
		t.Fatal(err)
	}
	readReply(t, conn)
	s := <-streamer.opened
	_ = conn.CloseNow()
	select {
	case <-s.ctx.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the session outlived the reader")
	}
}

// Speaking takes longer than the server's 15 s timeouts allow a request.
func TestSpeechStreamOutlastsTheServerTimeouts(t *testing.T) {
	users := notesUsers()
	ns := newNotesServer(t, NewAPIHandler(nil, nil, nil, users).WithSpeechStream(newFakeStreamer()), users)
	server := httptest.NewUnstartedServer(ns.router)
	server.Config.ReadTimeout = 100 * time.Millisecond
	server.Config.WriteTimeout = 100 * time.Millisecond
	server.Start()
	defer server.Close()

	conn := dialSpeech(t, server.URL)
	sendJSON(t, conn, map[string]string{"authorization": ns.auth})
	for i := 1; i <= 4; i++ {
		time.Sleep(100 * time.Millisecond)
		if err := conn.Write(context.Background(), websocket.MessageBinary, []byte{0, 0}); err != nil {
			t.Fatalf("piece %d: %v", i, err)
		}
		if r := readReply(t, conn); r.Text != fmt.Sprintf("слово %d", i) {
			t.Fatalf("piece %d: %+v", i, r)
		}
	}
}
