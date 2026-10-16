package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
)

func TestGenerationQueue_FreeSlotDoesNotWait(t *testing.T) {
	q := newGenerationQueue(1)

	release, err := q.acquire(context.Background(), func(int) {
		t.Error("onWait called while a slot was free")
	})
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	release()
}

func TestGenerationQueue_ReportsDocumentsAhead(t *testing.T) {
	q := newGenerationQueue(1)
	ctx := context.Background()

	releaseFirst, err := q.acquire(ctx, nil)
	if err != nil {
		t.Fatalf("acquire first: %v", err)
	}

	aheads := make(chan int, 2)
	acquired := make(chan func(), 2)
	for range 2 {
		waiting := make(chan struct{})
		go func() {
			release, err := q.acquire(ctx, func(ahead int) {
				aheads <- ahead
				close(waiting)
			})
			if err != nil {
				t.Errorf("acquire: %v", err)
				return
			}
			acquired <- release
		}()
		// Wait for this one to queue before starting the next, so the
		// positions are deterministic.
		<-waiting
	}

	if got := <-aheads; got != 1 {
		t.Errorf("second document: ahead = %d, want 1", got)
	}
	if got := <-aheads; got != 2 {
		t.Errorf("third document: ahead = %d, want 2", got)
	}

	select {
	case <-acquired:
		t.Fatal("a waiting document got a slot before the first released it")
	case <-time.After(50 * time.Millisecond):
	}

	releaseFirst()
	(<-acquired)()
	(<-acquired)()
}

func TestGenerationQueue_CancelledWhileWaiting(t *testing.T) {
	q := newGenerationQueue(1)
	release, err := q.acquire(context.Background(), nil)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	defer release()

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := q.acquire(ctx, func(int) { cancel() })
		done <- err
	}()

	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("err = %v, want context.Canceled", err)
		}
	case <-time.After(time.Second):
		t.Fatal("acquire did not return after ctx was cancelled")
	}

	// The cancelled document no longer counts as waiting.
	next, stop := context.WithCancel(context.Background())
	defer stop()
	got := make(chan int, 1)
	go func() {
		_, _ = q.acquire(next, func(ahead int) { got <- ahead })
	}()
	if ahead := <-got; ahead != 1 {
		t.Errorf("ahead after a cancelled waiter = %d, want 1", ahead)
	}
}

// recordingServer is a MAX Bot API stub that keeps the text of every sent
// and edited message. Handlers run concurrently, so access is locked.
type recordingServer struct {
	*httptest.Server
	mu    sync.Mutex
	texts []string
}

func newRecordingServer(t *testing.T) *recordingServer {
	t.Helper()
	s := &recordingServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/messages":
			var msg SendMessageRequest
			_ = json.NewDecoder(r.Body).Decode(&msg)
			s.mu.Lock()
			s.texts = append(s.texts, msg.Text)
			s.mu.Unlock()
			if r.Method == http.MethodPut {
				_, _ = w.Write([]byte(`{"success":true}`))
				return
			}
			_ = json.NewEncoder(w).Encode(SendMessageResponse{
				Message: &Message{Body: MessageBody{Mid: "mid-" + uuid.NewString()}},
			})
		case strings.HasPrefix(r.URL.Path, "/chats/"):
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *recordingServer) waitFor(t *testing.T, substr string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for _, text := range s.texts {
			if strings.Contains(text, substr) {
				s.mu.Unlock()
				return
			}
		}
		s.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	t.Fatalf("no message contains %q; got %q", substr, s.texts)
}

func fileUpdate(userID int64, name string) Update {
	return Update{
		UpdateType: "message_created",
		Message: &Message{
			Recipient: MessageRecipient{ChatID: userID},
			Sender:    User{UserID: userID, FirstName: "Student"},
			Body: MessageBody{
				Attachments: []Attachment{{
					Type:    "file",
					Payload: map[string]interface{}{"name": name, "data": []byte("notes")},
				}},
			},
		},
	}
}

func TestBot_DocumentInProgressDoesNotBlockOthers(t *testing.T) {
	server := newRecordingServer(t)
	client, err := NewClient("test-token", server.URL)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	started := make(chan struct{}, 2)
	unblock := make(chan struct{})
	base := &mockCardGenerator{}
	cardGen := &mockCardGenerator{
		generateFunc: func(ctx context.Context, doc generator.Document, onBatch func(generator.Batch)) (generator.Result, error) {
			started <- struct{}{}
			<-unblock
			return base.Generate(ctx, doc, onBatch)
		},
	}

	var savedMu sync.Mutex
	var saved []string
	setSvc := &mockSetService{
		saveGeneratedSetFunc: func(_ context.Context, _ int64, title string, _ generator.Result) (*contracts.CardSet, error) {
			savedMu.Lock()
			saved = append(saved, title)
			savedMu.Unlock()
			return &contracts.CardSet{ID: uuid.New(), Title: title}, nil
		},
	}

	b, err := NewBot(client, "https://example.test", &mockUserService{}, setSvc, &mockFileExtractor{}, cardGen)
	if err != nil {
		t.Fatalf("NewBot: %v", err)
	}
	ctx := context.Background()

	b.dispatch(ctx, fileUpdate(1, "first.pdf"))
	<-started

	b.dispatch(ctx, fileUpdate(2, "second.pdf"))
	b.dispatch(ctx, Update{UpdateType: "bot_started", ChatID: 3, User: &User{UserID: 3, FirstName: "New"}})

	// The welcome goes out while the first document is still being made.
	server.waitFor(t, "Привет! Я Помнибот")
	// The second document waits and says how many are ahead of it.
	server.waitFor(t, "перед тобой: 1")

	select {
	case <-started:
		t.Fatal("second document started while the first held the slot")
	case <-time.After(50 * time.Millisecond):
	}

	close(unblock)
	b.inflight.Wait()

	savedMu.Lock()
	defer savedMu.Unlock()
	if len(saved) != 2 || saved[0] != "first" || saved[1] != "second" {
		t.Errorf("saved sets = %q, want [first second] in arrival order", saved)
	}
}
