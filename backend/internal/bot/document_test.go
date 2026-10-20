package bot

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
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
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

// Two bots on the same API key queue into one line: the second document
// waits for the first even though another bot took it.
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

	bots := make([]*Bot, 2)
	for i := range bots {
		b, err := NewBot(client, "https://example.test", &mockUserService{}, &mockSetService{}, &mockFileExtractor{}, cardGen)
		if err != nil {
			t.Fatalf("NewBot: %v", err)
		}
		b.SetQueue(generator.SharedQueue(t.Name(), 1))
		bots[i] = b
	}
	ctx := context.Background()

	bots[0].dispatch(ctx, fileUpdate(1, "first.pdf"))
	<-started
	bots[1].dispatch(ctx, fileUpdate(2, "second.pdf"))
	server.waitFor(t, "перед тобой: 1")

	select {
	case <-started:
		t.Fatal("second bot started a document while the first held the key's slot")
	case <-time.After(50 * time.Millisecond):
	}

	close(unblock)
	bots[0].inflight.Wait()
	bots[1].inflight.Wait()
}

// Phone cameras and screenshots name files by date or number: such a name
// says nothing about the notes.
func TestIsGenericFilename(t *testing.T) {
	for name, want := range map[string]bool{
		"file_12":                        true,
		"file_12345.pdf":                 true,
		"photo_123.jpg":                  true,
		"IMG_1489.HEIC":                  true,
		"photo_2026-10-17 19.34.40.jpeg": true,
		"Снимок экрана 2026-10-17 в 21.18.32.png": true,
		"Screenshot 2026-10-17 at 21.18.32.png":   true,
		"scan0001.pdf":                            true,
		"Лекция 3. Клеточный цикл.pdf":            false,
		"konspekt.txt":                            false,
	} {
		if got := isGenericFilename(name); got != want {
			t.Errorf("isGenericFilename(%q) = %v, want %v", name, got, want)
		}
	}
}

// A file name becomes a title with spaces, not underscores.
func TestResolveSetTitleFromFilename(t *testing.T) {
	for name, want := range map[string]string{
		"лекция_биология.pdf": "лекция биология",
		"Этапы обработки кода программы на C++_ пространства имён и локали.pdf": "Этапы обработки кода программы на C++ пространства имён и локали",
		"__Лекция__4__.docx":           "Лекция 4",
		"Лекция 3. Клеточный цикл.pdf": "Лекция 3. Клеточный цикл",
		// A title or name with no extension keeps everything after its dots.
		"Лекция 3. Клеточный цикл": "Лекция 3. Клеточный цикл",
		"Версия 1.2":               "Версия 1.2",
	} {
		if got := resolveSetTitle("", name); got != want {
			t.Errorf("resolveSetTitle(%q) = %q, want %q", name, got, want)
		}
	}
}

// A generic name stays generic once its underscores turn into spaces,
// so the set is still renamed after its topics.
func TestIsGenericTitleAfterSpaces(t *testing.T) {
	for _, name := range []string{"file_12345.pdf", "IMG_1489.HEIC", "DSC_0042.jpg"} {
		if title := resolveSetTitle("", name); !isGenericTitle(title) {
			t.Errorf("isGenericTitle(resolveSetTitle(%q) = %q) = false, want true", name, title)
		}
	}
}

// A set made from photos with no caption is named after its commonest topic,
// not «Новый конспект (17.10 23:55)».
func TestTitleFromTopics(t *testing.T) {
	facts := []cards.Fact{{Topic: "Фазы митоза"}, {Topic: ""}, {Topic: "Клеточный цикл"}, {Topic: "Фазы митоза"}}
	if got := titleFromTopics(facts); got != "Фазы митоза" {
		t.Errorf("titleFromTopics = %q, want the commonest topic", got)
	}
	if got := titleFromTopics([]cards.Fact{{Topic: ""}}); got != "" {
		t.Errorf("titleFromTopics = %q, want none without topics", got)
	}
}
