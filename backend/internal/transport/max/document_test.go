package max

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/notes"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

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
		saveGeneratedSetFunc: func(_ context.Context, _ uuid.UUID, title string, _ generator.Result, _ []string) (*contracts.CardSet, error) {
			savedMu.Lock()
			saved = append(saved, title)
			savedMu.Unlock()
			return &contracts.CardSet{ID: uuid.New(), Title: title}, nil
		},
	}

	b, err := NewBot(client, "https://example.test", &mockUserService{}, notes.New(&mockFileExtractor{}, cardGen, setSvc, 1))
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

// Bots handed one notes.Maker queue into one line: the second document waits
// for the first even though another bot took it.
func TestBot_SharedQueueAcrossBots(t *testing.T) {
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

	maker := notes.New(&mockFileExtractor{}, cardGen, &mockSetService{}, 1)
	bots := make([]*Bot, 2)
	for i := range bots {
		b, err := NewBot(client, "https://example.test", &mockUserService{}, maker)
		if err != nil {
			t.Fatalf("NewBot: %v", err)
		}
		bots[i] = b
	}
	ctx := context.Background()

	bots[0].dispatch(ctx, fileUpdate(1, "first.pdf"))
	<-started
	bots[1].dispatch(ctx, fileUpdate(2, "second.pdf"))
	server.waitFor(t, "перед тобой: 1")

	select {
	case <-started:
		t.Fatal("second bot started a document while the first held the slot")
	case <-time.After(50 * time.Millisecond):
	}

	close(unblock)
	bots[0].inflight.Wait()
	bots[1].inflight.Wait()
}

func TestMAXUsesInternalIDForNotes(t *testing.T) {
	server := newRecordingServer(t)
	client, err := NewClient("test-token", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	testID900 := uuid.MustParse("00000000-0000-0000-0000-000000000900")
	var savedID uuid.UUID
	sets := &mockSetService{saveGeneratedSetFunc: func(_ context.Context, id uuid.UUID, title string, _ generator.Result, _ []string) (*contracts.CardSet, error) {
		savedID = id
		return &contracts.CardSet{ID: uuid.New(), Title: title}, nil
	}}
	users := &mockUserService{upsertMAXUserFunc: func(_ context.Context, i usecase.MAXUserParams) (usecase.Account, error) {
		if i.MaxID != 42 {
			t.Errorf("identity: %+v", i)
		}
		return usecase.Account{User: contracts.User{ID: testID900}, MaxID: 42}, nil
	}}
	bot, err := NewBot(client, "https://example.test", users, notes.New(&mockFileExtractor{}, &mockCardGenerator{}, sets, 1))
	if err != nil {
		t.Fatal(err)
	}
	bot.handleUpdate(context.Background(), fileUpdate(42, "note.pdf"))
	if savedID != testID900 {
		t.Fatalf("saved with external ID: %s", savedID)
	}
}
