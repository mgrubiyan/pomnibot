package telegram

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/notes"
	maxtransport "github.com/mgrubiyan/pomnibot/backend/internal/transport/max"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type testUsers struct {
	usecase.UserService
	mu      sync.Mutex
	account usecase.Account
	pending string
	inputs  []string
	calls   int
}

func (u *testUsers) UpsertTelegramUser(_ context.Context, params usecase.TelegramUserParams) (usecase.Account, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if params.TgID != 42 || !params.BotStarted {
		return usecase.Account{}, fmt.Errorf("unexpected Telegram input: %+v", params)
	}
	u.calls++
	return u.account, nil
}
func (u *testUsers) ProfileInput(_ context.Context, _ int64, text string, restart bool) (usecase.Account, error) {
	u.mu.Lock()
	defer u.mu.Unlock()
	if restart {
		u.account.ProfileStep = "last_name"
		u.pending = ""
	} else {
		var err error
		text, err = usecase.CleanProfileName(text)
		if err != nil {
			return u.account, err
		}
		u.inputs = append(u.inputs, text)
		switch u.account.ProfileStep {
		case "last_name":
			u.pending = text
			u.account.ProfileStep = "first_name"
		case "first_name":
			u.account.User.FirstName = text
			u.account.User.LastName.SetTo(u.pending)
			u.account.ProfileComplete = true
			u.account.ProfileStep = ""
		}
	}
	return u.account, nil
}
func (u *testUsers) GetUsersWithDueFacts(context.Context, time.Time, string) ([]int64, error) {
	return []int64{900, 901}, nil
}
func (u *testUsers) GetAccount(_ context.Context, id int64) (usecase.Account, error) {
	if id == 900 {
		return usecase.Account{TgID: 43, BotStarted: true, ProfileComplete: true}, nil
	}
	if id == 901 {
		return usecase.Account{MaxID: 43, ProfileComplete: true}, nil
	}
	return usecase.Account{}, fmt.Errorf("wrong internal ID %d", id)
}

type extractorFunc func(context.Context, []ingest.File) (ingest.Result, error)

func (f extractorFunc) Extract(ctx context.Context, files []ingest.File) (ingest.Result, error) {
	return f(ctx, files)
}

type generatorStub struct{}

func (generatorStub) Generate(context.Context, generator.Document, func(generator.Batch)) (generator.Result, error) {
	return generator.Result{}, nil
}

type saverStub struct{}

func (saverStub) SaveGeneratedSet(context.Context, int64, string, generator.Result) (*contracts.CardSet, error) {
	return &contracts.CardSet{}, nil
}

func testMessage(id int64, text string) *Message {
	m := &Message{ID: id, Text: text, From: &User{ID: 42, FirstName: "Remote"}}
	m.Chat.ID = 42
	m.Chat.Type = "private"
	return m
}
func testBot(t *testing.T, users *testUsers, handler http.HandlerFunc, extractor notes.Extractor) (*Bot, *httptest.Server) {
	t.Helper()
	server := httptest.NewServer(handler)
	client := NewClient("secret")
	client.baseURL = server.URL
	bot, err := NewBot(client, users, notes.New(extractor, generatorStub{}, saverStub{}, 1), "https://example.test", maxtransport.NotificationConfig{})
	if err != nil {
		server.Close()
		t.Fatal(err)
	}
	t.Cleanup(server.Close)
	return bot, server
}
func sendOK(w http.ResponseWriter) { _, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`)) }

func TestTelegramProfileDialogue(t *testing.T) {
	users := &testUsers{account: usecase.Account{User: contracts.User{ID: 900}, TgID: 42, ProfileStep: "last_name"}}
	var texts []string
	bot, _ := testBot(t, users, func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Text string }
		_ = json.NewDecoder(r.Body).Decode(&body)
		texts = append(texts, body.Text)
		sendOK(w)
	}, nil)
	ctx := context.Background()
	for _, m := range []*Message{testMessage(1, "/start"), testMessage(2, "/unknown"), testMessage(3, "")} {
		if m.ID == 3 {
			m.Document = &File{ID: "doc", Name: "note.pdf"}
			m.Caption = "Attachment is not a name"
		}
		if err := bot.handleMessage(ctx, m); err != nil {
			t.Fatal(err)
		}
	}
	if len(users.inputs) != 0 {
		t.Fatal("command or attachment accepted as profile answer")
	}
	if err := bot.handleMessage(ctx, testMessage(4, "  Фамилия  ")); err != nil {
		t.Fatal(err)
	}
	if err := bot.handleMessage(ctx, testMessage(5, strings.Repeat("я", 101))); err != nil {
		t.Fatal(err)
	}
	if users.account.ProfileStep != "first_name" {
		t.Fatal("invalid name advanced dialogue")
	}
	if err := bot.handleMessage(ctx, testMessage(6, "Имя")); err != nil {
		t.Fatal(err)
	}
	if !users.account.ProfileComplete || users.account.User.ID != 900 || users.account.User.LastName.Value != "Фамилия" {
		t.Fatalf("profile: %+v", users.account)
	}
	if err := bot.handleMessage(ctx, testMessage(7, "/profile")); err != nil {
		t.Fatal(err)
	}
	if err := bot.handleMessage(ctx, testMessage(8, "Новая")); err != nil {
		t.Fatal(err)
	}
	if err := bot.handleMessage(ctx, testMessage(9, "/start")); err != nil {
		t.Fatal(err)
	}
	if users.account.User.LastName.Value != "Фамилия" || users.account.ProfileStep != "first_name" {
		t.Fatal("partial update changed saved profile")
	}
	if !strings.Contains(texts[len(texts)-1], "имя") {
		t.Fatalf("start did not resume: %s", texts[len(texts)-1])
	}
}

func TestTelegramAlbumAndPhotos(t *testing.T) {
	users := &testUsers{account: usecase.Account{User: contracts.User{ID: 900}, TgID: 42, ProfileComplete: true}}
	var mu sync.Mutex
	var downloads []string
	extracted := make(chan []ingest.File, 2)
	bot, _ := testBot(t, users, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "getFile"):
			var body struct {
				ID string `json:"file_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			downloads = append(downloads, body.ID)
			mu.Unlock()
			_, _ = fmt.Fprintf(w, `{"ok":true,"result":{"file_path":"%s","file_size":4}}`, body.ID)
		case strings.Contains(r.URL.Path, "/file/"):
			_, _ = w.Write([]byte("text"))
		default:
			sendOK(w)
		}
	}, extractorFunc(func(_ context.Context, files []ingest.File) (ingest.Result, error) {
		extracted <- files
		return ingest.Result{}, nil
	}))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	first := testMessage(1, "")
	first.MediaGroupID = "album"
	first.Photos = []File{{ID: "small", Width: 100, Height: 100}, {ID: "large", Width: 1000, Height: 1000}, {ID: "medium", Width: 500, Height: 500}}
	second := testMessage(2, "")
	second.MediaGroupID = "album"
	second.Document = &File{ID: "doc", Name: "notes.pdf"}
	if err := bot.handleMessage(ctx, first); err != nil {
		t.Fatal(err)
	}
	if err := bot.handleMessage(ctx, second); err != nil {
		t.Fatal(err)
	}
	select {
	case files := <-extracted:
		if len(files) != 2 || files[0].Name != "photo.jpg" || files[1].Name != "notes.pdf" {
			t.Fatalf("album: %+v", files)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("album not processed")
	}
	bot.jobs.Wait()
	mu.Lock()
	defer mu.Unlock()
	if strings.Join(downloads, ",") != "large,doc" {
		t.Fatalf("photo selection: %v", downloads)
	}
}

func TestTelegramRejectsOversizeAndDownloadErrors(t *testing.T) {
	for _, tooLarge := range []bool{true, false} {
		t.Run(fmt.Sprint(tooLarge), func(t *testing.T) {
			users := &testUsers{}
			var texts []string
			bot, _ := testBot(t, users, func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "getFile") {
					w.WriteHeader(500)
					_, _ = w.Write([]byte(`{"ok":false,"error_code":500}`))
					return
				}
				var body struct{ Text string }
				_ = json.NewDecoder(r.Body).Decode(&body)
				texts = append(texts, body.Text)
				sendOK(w)
			}, extractorFunc(func(context.Context, []ingest.File) (ingest.Result, error) {
				t.Error("unexpected extraction")
				return ingest.Result{}, nil
			}))
			message := testMessage(1, "")
			message.Document = &File{ID: "file", Name: "notes.txt"}
			if tooLarge {
				message.Document.Size = MaxFileSize + 1
			}
			bot.process(context.Background(), 900, []*Message{message})
			expected := notes.TextResend
			if tooLarge {
				expected = textLargeFile
			}
			if texts[len(texts)-1] != expected {
				t.Fatalf("%v", texts)
			}
		})
	}
}

func TestPollingDeduplicatesAndOrdersDialogue(t *testing.T) {
	users := &testUsers{account: usecase.Account{User: contracts.User{ID: 900}, TgID: 42, ProfileStep: "last_name"}}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	polls := 0
	bot, _ := testBot(t, users, func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "getUpdates") {
			polls++
			if polls == 1 {
				_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": []Update{{ID: 10, Message: testMessage(1, "Last")}, {ID: 10, Message: testMessage(1, "Last")}, {ID: 11, Message: testMessage(2, "First")}}})
			} else {
				cancel()
				_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
			}
			return
		}
		sendOK(w)
	}, nil)
	bot.poll(ctx)
	bot.dialogues.Wait()
	// Every profile update is applied before the next polling confirmation.
	users.mu.Lock()
	calls := users.calls
	users.mu.Unlock()
	if calls != 2 {
		t.Fatalf("duplicate update processed: %d", calls)
	}
	if strings.Join(users.inputs, ",") != "Last,First" {
		t.Fatalf("duplicate profile input: %v", users.inputs)
	}
	ctx = context.Background()
	users.account.ProfileStep = "last_name"
	users.inputs = nil
	bot.dispatch(ctx, Update{ID: 20, Message: testMessage(3, "New Last")})
	bot.dispatch(ctx, Update{ID: 21, Message: testMessage(4, "New First")})
	bot.dialogues.Wait()
	if strings.Join(users.inputs, ",") != "New Last,New First" {
		t.Fatalf("dialogue order: %v", users.inputs)
	}
}

func TestTelegramRemindersUseExternalIdentity(t *testing.T) {
	var chatIDs []int64
	bot, _ := testBot(t, &testUsers{}, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChatID int64 `json:"chat_id"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		chatIDs = append(chatIDs, body.ChatID)
		sendOK(w)
	}, nil)
	count, err := bot.SendReviewReminders(context.Background())
	if err != nil || count != 1 || len(chatIDs) != 1 || chatIDs[0] != 43 {
		t.Fatalf("reminders: %v %d %v", chatIDs, count, err)
	}
}

func TestAlbumShutdown(t *testing.T) {
	bot, _ := testBot(t, &testUsers{}, func(w http.ResponseWriter, _ *http.Request) { sendOK(w) }, nil)
	ctx, cancel := context.WithCancel(context.Background())
	m := testMessage(1, "")
	m.Photos = []File{{ID: "photo", Width: 100, Height: 100}}
	m.MediaGroupID = "pending"
	bot.addAlbum(ctx, 900, m)
	cancel()
	done := make(chan struct{})
	go func() { bot.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("album did not stop")
	}
	bot.mu.Lock()
	defer bot.mu.Unlock()
	if len(bot.albums) != 0 {
		t.Fatal("album retained after shutdown")
	}

}

type generatingFunc func(context.Context, generator.Document, func(generator.Batch)) (generator.Result, error)

func (f generatingFunc) Generate(ctx context.Context, doc generator.Document, onBatch func(generator.Batch)) (generator.Result, error) {
	return f(ctx, doc, onBatch)
}

type savingFunc func(context.Context, int64, string, generator.Result) (*contracts.CardSet, error)

func (f savingFunc) SaveGeneratedSet(ctx context.Context, id int64, title string, result generator.Result) (*contracts.CardSet, error) {
	return f(ctx, id, title, result)
}

func TestTelegramUsesSharedMakerAndInternalUserID(t *testing.T) {
	var mu sync.Mutex
	var statuses []string
	queued := make(chan struct{}, 1)
	bot, _ := testBot(t, &testUsers{}, func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "getFile"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"file_path":"note.txt","file_size":4}}`))
		case strings.Contains(r.URL.Path, "/file/"):
			_, _ = w.Write([]byte("text"))
		default:
			var body struct {
				ChatID int64  `json:"chat_id"`
				Text   string `json:"text"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.ChatID != 42 {
				t.Errorf("sent to internal ID: %d", body.ChatID)
			}
			mu.Lock()
			statuses = append(statuses, body.Text)
			mu.Unlock()
			if strings.Contains(body.Text, "перед тобой: 1") {
				select {
				case queued <- struct{}{}:
				default:
				}
			}
			sendOK(w)
		}
	}, nil)
	started := make(chan struct{}, 2)
	unblock := make(chan struct{})
	var savedIDs []int64
	maker := notes.New(extractorFunc(func(context.Context, []ingest.File) (ingest.Result, error) {
		return ingest.Result{Text: "material"}, nil
	}),
		generatingFunc(func(ctx context.Context, _ generator.Document, onBatch func(generator.Batch)) (generator.Result, error) {
			started <- struct{}{}
			select {
			case <-unblock:
			case <-ctx.Done():
				return generator.Result{}, ctx.Err()
			}
			card := cards.Card{FactID: "fact", Question: "Question", Answer: "Answer"}
			onBatch(generator.Batch{Cards: []cards.Card{card}})
			return generator.Result{Cards: []cards.Card{card}}, nil
		}), savingFunc(func(_ context.Context, id int64, title string, _ generator.Result) (*contracts.CardSet, error) {
			savedIDs = append(savedIDs, id)
			return &contracts.CardSet{Title: title, ShareCode: contracts.NewOptString("123456")}, nil
		}), 1)
	bot.maker = maker
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// An existing MAX job holds the application Maker's only slot.
	maxDone := make(chan error, 1)
	go func() {
		_, err := maker.Make(ctx, notes.Request{UserID: 800, Files: []ingest.File{{Name: "max.txt", Data: []byte("text")}}}, notes.Progress{})
		maxDone <- err
	}()
	<-started
	message := testMessage(1, "")
	message.Document = &File{ID: "doc", Name: "telegram.txt"}
	telegramDone := make(chan struct{})
	go func() { bot.process(ctx, 900, []*Message{message}); close(telegramDone) }()
	select {
	case <-queued:
	case <-time.After(2 * time.Second):
		close(unblock)
		t.Fatal("Telegram did not join shared queue")
	}
	select {
	case <-started:
		close(unblock)
		t.Fatal("Telegram bypassed shared queue")
	default:
	}
	close(unblock)
	if err := <-maxDone; err != nil {
		t.Fatal(err)
	}
	<-telegramDone
	if len(savedIDs) != 2 || savedIDs[0] != 800 || savedIDs[1] != 900 {
		t.Fatalf("saved IDs: %v", savedIDs)
	}
	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(statuses[len(statuses)-1], "123456") {
		t.Fatalf("missing share code: %v", statuses)
	}
}
