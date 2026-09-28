package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/contracts"
	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/models/cards"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type mockUserService struct {
	upsertUserFunc func(ctx context.Context, params usecase.UpsertUserParams) error
	ensureUserFunc func(ctx context.Context, userID int64) error
}

func (m *mockUserService) UpsertUser(ctx context.Context, params usecase.UpsertUserParams) error {
	if m.upsertUserFunc != nil {
		return m.upsertUserFunc(ctx, params)
	}
	return nil
}

func (m *mockUserService) EnsureUser(ctx context.Context, userID int64) error {
	if m.ensureUserFunc != nil {
		return m.ensureUserFunc(ctx, userID)
	}
	return nil
}

type mockSetService struct {
	usecase.SetService
	generateMockSetFunc  func(ctx context.Context, userID int64, title string) (*contracts.CardSet, error)
	saveGeneratedSetFunc func(ctx context.Context, userID int64, title string, genResult generator.Result) (*contracts.CardSet, error)
}

func (m *mockSetService) GenerateMockSet(ctx context.Context, userID int64, title string) (*contracts.CardSet, error) {
	if m.generateMockSetFunc != nil {
		return m.generateMockSetFunc(ctx, userID, title)
	}
	res := &contracts.CardSet{
		ID:         uuid.New(),
		Title:      title,
		CardsTotal: 66,
		CardsDue:   66,
	}
	res.ShareCode.SetTo("987654")
	return res, nil
}

func (m *mockSetService) SaveGeneratedSet(ctx context.Context, userID int64, title string, genResult generator.Result) (*contracts.CardSet, error) {
	if m.saveGeneratedSetFunc != nil {
		return m.saveGeneratedSetFunc(ctx, userID, title, genResult)
	}
	res := &contracts.CardSet{
		ID:         uuid.New(),
		Title:      title,
		CardsTotal: len(genResult.Cards),
		CardsDue:   len(genResult.Cards),
	}
	res.ShareCode.SetTo("654321")
	return res, nil
}

type mockFileExtractor struct {
	extractFunc func(ctx context.Context, files []ingest.File) (ingest.Result, error)
}

func (m *mockFileExtractor) Extract(ctx context.Context, files []ingest.File) (ingest.Result, error) {
	if m.extractFunc != nil {
		return m.extractFunc(ctx, files)
	}
	pages := make([]ingest.PageInfo, len(files))
	for i := range files {
		pages[i] = ingest.PageInfo{
			Number: i + 1,
			Start:  i * 100,
		}
	}
	return ingest.Result{
		Text:  "Sample extracted text from notes",
		Pages: pages,
	}, nil
}

type mockCardGenerator struct {
	generateFunc func(ctx context.Context, doc generator.Document, onBatch func(generator.Batch)) (generator.Result, error)
}

func (m *mockCardGenerator) Generate(ctx context.Context, doc generator.Document, onBatch func(generator.Batch)) (generator.Result, error) {
	if m.generateFunc != nil {
		return m.generateFunc(ctx, doc, onBatch)
	}
	res := generator.Result{
		Facts: []cards.Fact{
			{ID: "fact-1", Name: "Fact One", Topic: "Biology"},
		},
		Cards: []cards.Card{
			{
				FactID:      "fact-1",
				Kind:        cards.KindChoice,
				Question:    "Test question",
				Options:     []string{"A", "B"},
				Answer:      "A",
				Explanation: "Test explanation",
				SourceQuote: "Sample quote",
			},
		},
	}
	if onBatch != nil {
		onBatch(generator.Batch{Facts: res.Facts, Cards: res.Cards})
	}
	return res, nil
}

func TestNewBot_Validation(t *testing.T) {
	client := &Client{}
	userSvc := &mockUserService{}
	setSvc := &mockSetService{}
	extractor := &mockFileExtractor{}
	cardGen := &mockCardGenerator{}

	t.Run("nil client returns error", func(t *testing.T) {
		b, err := NewBot(nil, "http://localhost", userSvc, setSvc, extractor, cardGen)
		if err == nil || b != nil {
			t.Fatalf("expected error on nil client, got %v", err)
		}
	})

	t.Run("nil user service returns error", func(t *testing.T) {
		b, err := NewBot(client, "http://localhost", nil, setSvc, extractor, cardGen)
		if err == nil || b != nil {
			t.Fatalf("expected error on nil user service, got %v", err)
		}
	})

	t.Run("nil set service returns error", func(t *testing.T) {
		b, err := NewBot(client, "http://localhost", userSvc, nil, extractor, cardGen)
		if err == nil || b != nil {
			t.Fatalf("expected error on nil set service, got %v", err)
		}
	})

	t.Run("valid parameters returns Bot", func(t *testing.T) {
		b, err := NewBot(client, "", userSvc, setSvc, extractor, cardGen)
		if err != nil || b == nil {
			t.Fatalf("expected success, got error %v", err)
		}
		if b.appURL != "https://pomnibot.steins.ru" {
			t.Errorf("expected default appURL, got %s", b.appURL)
		}
	})
}

func TestBotWelcomeMessage(t *testing.T) {
	var sentMessage SendMessageRequest
	var receivedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth = r.Header.Get("Authorization")

		switch r.URL.Path {
		case "/me":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(User{
				UserID:    387935087,
				Username:  ref("t583_hakaton_max_bot"),
				FirstName: "Хакатон МАХ",
				IsBot:     true,
			})
		case "/subscriptions":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(SubscriptionsResponse{Subscriptions: []Subscription{}})
		case "/messages":
			_ = json.NewDecoder(r.Body).Decode(&sentMessage)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient("test-token", server.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	var capturedParams *usecase.UpsertUserParams
	userSvc := &mockUserService{
		upsertUserFunc: func(_ context.Context, params usecase.UpsertUserParams) error {
			capturedParams = &params
			return nil
		},
	}
	setSvc := &mockSetService{}

	bot, err := NewBot(client, "https://pomnibot.steins.ru", userSvc, setSvc, &mockFileExtractor{}, &mockCardGenerator{})
	if err != nil {
		t.Fatalf("NewBot failed: %v", err)
	}
	ctx := context.Background()

	me, err := client.GetMe(ctx)
	if err != nil {
		t.Fatalf("GetMe failed: %v", err)
	}
	bot.botUser = me

	// Test update handling for bot_started
	bot.handleUpdate(ctx, Update{
		UpdateType: "bot_started",
		ChatID:     12345,
		User: &User{
			UserID:    999,
			FirstName: "Test",
			LastName:  ref("User"),
			Username:  ref("testuser"),
			IsBot:     false,
		},
	})

	if receivedAuth != "test-token" {
		t.Errorf("expected Authorization 'test-token', got '%s'", receivedAuth)
	}

	if capturedParams == nil || capturedParams.ID != 999 {
		t.Fatalf("expected UpsertUser to be called for user 999, got %+v", capturedParams)
	}
	if capturedParams.FirstName != "Test" || *capturedParams.LastName != "User" {
		t.Errorf("unexpected params: %+v", capturedParams)
	}

	if len(sentMessage.Attachments) == 0 {
		t.Fatalf("expected attachments in welcome message")
	}

	buttons, ok := sentMessage.Attachments[0].Payload["buttons"].([]interface{})
	if !ok || len(buttons) < 2 {
		t.Fatalf("expected at least 2 button rows, got %+v", sentMessage.Attachments[0].Payload["buttons"])
	}

	row1 := buttons[0].([]interface{})
	firstBtn := row1[0].(map[string]interface{})
	if firstBtn["type"] != "open_app" || firstBtn["web_app"] != "t583_hakaton_max_bot" {
		t.Errorf("unexpected first button: %+v", firstBtn)
	}

	row2 := buttons[1].([]interface{})
	secondBtn := row2[0].(map[string]interface{})
	if secondBtn["type"] != "link" || secondBtn["url"] != "https://pomnibot.steins.ru" {
		t.Errorf("unexpected second button: %+v", secondBtn)
	}
}

func TestBot_UpsertUser_OnMessageCreated(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/me":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(User{
				UserID:    387935087,
				Username:  ref("t583_hakaton_max_bot"),
				FirstName: "Хакатон МАХ",
				IsBot:     true,
			})
		case "/messages":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient("test-token", server.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	var capturedParams *usecase.UpsertUserParams
	userSvc := &mockUserService{
		upsertUserFunc: func(_ context.Context, params usecase.UpsertUserParams) error {
			capturedParams = &params
			return nil
		},
	}
	setSvc := &mockSetService{}

	bot, err := NewBot(client, "https://pomnibot.steins.ru", userSvc, setSvc, &mockFileExtractor{}, &mockCardGenerator{})
	if err != nil {
		t.Fatalf("NewBot failed: %v", err)
	}
	ctx := context.Background()
	bot.botUser = &User{UserID: 387935087, IsBot: true}

	t.Run("upserts user on normal user message", func(t *testing.T) {
		capturedParams = nil
		bot.handleUpdate(ctx, Update{
			UpdateType: "message_created",
			Message: &Message{
				Recipient: MessageRecipient{ChatID: 100},
				Sender: User{
					UserID:    555,
					FirstName: "Bob",
					LastName:  ref("Builder"),
					Username:  ref("bobbuilder"),
					IsBot:     false,
				},
				Body: MessageBody{Text: "hello"},
			},
		})

		if capturedParams == nil {
			t.Fatalf("expected UpsertUser to be called")
		}
		if capturedParams.ID != 555 || capturedParams.FirstName != "Bob" {
			t.Errorf("unexpected captured params: %+v", capturedParams)
		}
	})

	t.Run("ignores message from bot sender", func(t *testing.T) {
		capturedParams = nil
		bot.handleUpdate(ctx, Update{
			UpdateType: "message_created",
			Message: &Message{
				Recipient: MessageRecipient{ChatID: 100},
				Sender: User{
					UserID:    888,
					FirstName: "OtherBot",
					IsBot:     true,
				},
				Body: MessageBody{Text: "bot message"},
			},
		})

		if capturedParams != nil {
			t.Fatalf("expected UpsertUser NOT to be called for bot sender, got %+v", capturedParams)
		}
	})
}

func TestBot_UpsertUser_ResilientOnError(t *testing.T) {
	var messageSent bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/me":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(User{
				UserID:    387935087,
				Username:  ref("t583_hakaton_max_bot"),
				FirstName: "Хакатон МАХ",
				IsBot:     true,
			})
		case "/messages":
			messageSent = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient("test-token", server.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	userSvc := &mockUserService{
		upsertUserFunc: func(_ context.Context, _ usecase.UpsertUserParams) error {
			return errors.New("database connection unavailable")
		},
	}
	setSvc := &mockSetService{}

	bot, err := NewBot(client, "https://pomnibot.steins.ru", userSvc, setSvc, &mockFileExtractor{}, &mockCardGenerator{})
	if err != nil {
		t.Fatalf("NewBot failed: %v", err)
	}
	ctx := context.Background()
	bot.botUser = &User{UserID: 387935087, IsBot: true}

	bot.handleUpdate(ctx, Update{
		UpdateType: "message_created",
		Message: &Message{
			Recipient: MessageRecipient{ChatID: 200},
			Sender: User{
				UserID:    777,
				FirstName: "Resilient",
				IsBot:     false,
			},
			Body: MessageBody{Text: "hi"},
		},
	})

	if !messageSent {
		t.Fatalf("expected message to still be sent even if UpsertUser returned an error")
	}
}

func TestBot_FileAttachment_GeneratesSet(t *testing.T) {
	var sentMessage SendMessageRequest
	var editedMessage SendMessageRequest
	var editedMIDs []string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/me":
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(User{
				UserID:    387935087,
				Username:  ref("test_bot"),
				FirstName: "Test Bot",
				IsBot:     true,
			})
		case r.URL.Path == "/messages" && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&sentMessage)
			w.WriteHeader(http.StatusOK)
			_ = json.NewEncoder(w).Encode(SendMessageResponse{
				Message: &Message{
					Body: MessageBody{Mid: "mid-test-123"},
				},
			})
		case r.URL.Path == "/messages" && r.Method == http.MethodPut:
			editedMIDs = append(editedMIDs, r.URL.Query().Get("message_id"))
			_ = json.NewDecoder(r.Body).Decode(&editedMessage)
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}`))
		case strings.HasPrefix(r.URL.Path, "/download"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Sample notes text"))
		case strings.HasPrefix(r.URL.Path, "/chats/"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client, err := NewClient("test-token", server.URL)
	if err != nil {
		t.Fatalf("failed to create client: %v", err)
	}

	userSvc := &mockUserService{}
	var capturedUserID int64
	var capturedTitle string

	setSvc := &mockSetService{
		saveGeneratedSetFunc: func(_ context.Context, userID int64, title string, _ generator.Result) (*contracts.CardSet, error) {
			capturedUserID = userID
			capturedTitle = title
			res := &contracts.CardSet{
				ID:         uuid.New(),
				Title:      title,
				CardsTotal: 10,
				CardsDue:   10,
			}
			res.ShareCode.SetTo("654321")
			return res, nil
		},
	}
	extractor := &mockFileExtractor{}
	cardGen := &mockCardGenerator{}

	bot, err := NewBot(client, "https://pomnibot.steins.ru", userSvc, setSvc, extractor, cardGen)
	if err != nil {
		t.Fatalf("NewBot failed: %v", err)
	}
	ctx := context.Background()
	bot.botUser = &User{UserID: 387935087, Username: ref("test_bot"), IsBot: true}

	getLastMessage := func() SendMessageRequest {
		if editedMessage.Text != "" {
			return editedMessage
		}
		return sentMessage
	}

	t.Run("file attachment generates set with title from filename", func(t *testing.T) {
		sentMessage = SendMessageRequest{}
		editedMessage = SendMessageRequest{}
		capturedUserID = 0
		capturedTitle = ""

		bot.handleUpdate(ctx, Update{
			UpdateType: "message_created",
			Message: &Message{
				Recipient: MessageRecipient{ChatID: 300},
				Sender: User{
					UserID:    12345,
					FirstName: "Alice",
					IsBot:     false,
				},
				Body: MessageBody{
					Text: "",
					Attachments: []Attachment{
						{
							Type: "file",
							Payload: map[string]interface{}{
								"name": "лекция_биология.pdf",
								"data": []byte("text content"),
							},
						},
					},
				},
			},
		})

		if capturedUserID != 12345 {
			t.Errorf("expected userID 12345, got %d", capturedUserID)
		}
		if capturedTitle != "лекция_биология" {
			t.Errorf("expected title 'лекция_биология', got %q", capturedTitle)
		}
		lastMsg := getLastMessage()
		if !strings.Contains(lastMsg.Text, "🎉 Набор «лекция_биология» успешно создан!") {
			t.Errorf("expected confirmation text, got: %s", lastMsg.Text)
		}
		if !strings.Contains(lastMsg.Text, "Код для совместного доступа: 654321") {
			t.Errorf("expected share code in text, got: %s", lastMsg.Text)
		}
		if len(lastMsg.Attachments) == 0 {
			t.Fatalf("expected inline keyboard buttons in response")
		}
	})

	t.Run("message caption overrides filename for title", func(t *testing.T) {
		sentMessage = SendMessageRequest{}
		editedMessage = SendMessageRequest{}
		capturedUserID = 0
		capturedTitle = ""

		bot.handleUpdate(ctx, Update{
			UpdateType: "message_created",
			Message: &Message{
				Recipient: MessageRecipient{ChatID: 300},
				Sender: User{
					UserID:    12345,
					FirstName: "Alice",
					IsBot:     false,
				},
				Body: MessageBody{
					Text: "Курс по микробиологии",
					Attachments: []Attachment{
						{
							Type: "image",
							Payload: map[string]interface{}{
								"name": "photo_123.jpg",
								"data": []byte("photo content"),
							},
						},
					},
				},
			},
		})

		if capturedTitle != "Курс по микробиологии" {
			t.Errorf("expected title 'Курс по микробиологии', got %q", capturedTitle)
		}
		lastMsg := getLastMessage()
		if !strings.Contains(lastMsg.Text, "🎉 Набор «Курс по микробиологии» успешно создан!") {
			t.Errorf("expected confirmation text, got: %s", lastMsg.Text)
		}
	})

	t.Run("attachment at message root level recognized", func(t *testing.T) {
		sentMessage = SendMessageRequest{}
		editedMessage = SendMessageRequest{}
		capturedTitle = ""

		bot.handleUpdate(ctx, Update{
			UpdateType: "message_created",
			Message: &Message{
				Recipient: MessageRecipient{ChatID: 300},
				Sender: User{
					UserID:    12345,
					FirstName: "Alice",
					IsBot:     false,
				},
				Body: MessageBody{Text: ""},
				Attachments: []Attachment{
					{
						Type: "file",
						Payload: map[string]interface{}{
							"name": "document.txt",
							"data": []byte("doc content"),
						},
					},
				},
			},
		})

		if capturedTitle != "document" {
			t.Errorf("expected title 'document', got %q", capturedTitle)
		}
	})

	t.Run("batch of photos generates joint set", func(t *testing.T) {
		sentMessage = SendMessageRequest{}
		editedMessage = SendMessageRequest{}
		capturedUserID = 0
		capturedTitle = ""

		var extractedFiles []ingest.File
		extractor.extractFunc = func(_ context.Context, files []ingest.File) (ingest.Result, error) {
			extractedFiles = files
			return ingest.Result{
				Text: "Extracted joint text from 3 photos",
				Pages: []ingest.PageInfo{
					{Number: 1, Start: 0},
					{Number: 2, Start: 100},
					{Number: 3, Start: 200},
				},
			}, nil
		}

		bot.handleUpdate(ctx, Update{
			UpdateType: "message_created",
			Message: &Message{
				Recipient: MessageRecipient{ChatID: 300},
				Sender: User{
					UserID:    12345,
					FirstName: "Alice",
					IsBot:     false,
				},
				Body: MessageBody{
					Text: "Лекция по физике",
					Attachments: []Attachment{
						{
							Type: "photo",
							Payload: map[string]interface{}{
								"name": "page1.jpg",
								"data": []byte("photo1"),
							},
						},
						{
							Type: "photo",
							Payload: map[string]interface{}{
								"name": "page2.jpg",
								"data": []byte("photo2"),
							},
						},
						{
							Type: "photo",
							Payload: map[string]interface{}{
								"name": "page3.jpg",
								"data": []byte("photo3"),
							},
						},
					},
				},
			},
		})

		if len(extractedFiles) != 3 {
			t.Fatalf("expected 3 files extracted, got %d", len(extractedFiles))
		}
		if capturedTitle != "Лекция по физике" {
			t.Errorf("expected title 'Лекция по физике', got %q", capturedTitle)
		}
		lastMsg := getLastMessage()
		if !strings.Contains(lastMsg.Text, "🎉 Набор «Лекция по физике» успешно создан!") {
			t.Errorf("expected confirmation text, got: %s", lastMsg.Text)
		}
	})

	t.Run("error handling on generate failure", func(t *testing.T) {
		sentMessage = SendMessageRequest{}
		editedMessage = SendMessageRequest{}
		cardGen.generateFunc = func(_ context.Context, _ generator.Document, _ func(generator.Batch)) (generator.Result, error) {
			return generator.Result{}, errors.New("generation error")
		}
		defer func() { cardGen.generateFunc = nil }()

		bot.handleUpdate(ctx, Update{
			UpdateType: "message_created",
			Message: &Message{
				Recipient: MessageRecipient{ChatID: 300},
				Sender: User{
					UserID:    12345,
					FirstName: "Alice",
					IsBot:     false,
				},
				Body: MessageBody{
					Attachments: []Attachment{
						{
							Type: "file",
							Payload: map[string]interface{}{
								"name": "test.txt",
								"data": []byte("test content"),
							},
						},
					},
				},
			},
		})

		lastMsg := getLastMessage()
		if !strings.Contains(lastMsg.Text, "ошибка при создании конспекта") {
			t.Errorf("expected error message sent to user, got: %s", lastMsg.Text)
		}
	})

	t.Run("root-level russian filename on attachment used for title", func(t *testing.T) {
		sentMessage = SendMessageRequest{}
		editedMessage = SendMessageRequest{}
		capturedTitle = ""

		bot.handleUpdate(ctx, Update{
			UpdateType: "message_created",
			Message: &Message{
				Recipient: MessageRecipient{ChatID: 300},
				Sender: User{
					UserID:    12345,
					FirstName: "Alice",
					IsBot:     false,
				},
				Body: MessageBody{
					Text: "",
					Attachments: []Attachment{
						{
							Type:     "file",
							Filename: "лекция_биология.pdf",
							Payload: map[string]interface{}{
								"data": []byte("text content"),
							},
						},
					},
				},
			},
		})

		if capturedTitle != "лекция_биология" {
			t.Errorf("expected title 'лекция_биология', got %q", capturedTitle)
		}
		lastMsg := getLastMessage()
		if !strings.Contains(lastMsg.Text, "🎉 Набор «лекция_биология» успешно создан!") {
			t.Errorf("expected confirmation text with title, got: %s", lastMsg.Text)
		}
	})

	t.Run("single and double percent-encoded russian filename properly decoded", func(t *testing.T) {
		// Single URL-encoded
		sentMessage = SendMessageRequest{}
		editedMessage = SendMessageRequest{}
		capturedTitle = ""

		bot.handleUpdate(ctx, Update{
			UpdateType: "message_created",
			Message: &Message{
				Recipient: MessageRecipient{ChatID: 300},
				Sender: User{
					UserID:    12345,
					FirstName: "Alice",
					IsBot:     false,
				},
				Body: MessageBody{
					Text: "",
					Attachments: []Attachment{
						{
							Type:     "file",
							Filename: "%D0%BB%D0%B5%D0%BA%D1%86%D0%B8%D1%8F_%D0%B1%D0%B8%D0%BE%D0%BB%D0%BE%D0%B3%D0%B8%D1%8F.pdf",
							Payload: map[string]interface{}{
								"data": []byte("text content"),
							},
						},
					},
				},
			},
		})

		if capturedTitle != "лекция_биология" {
			t.Errorf("expected title 'лекция_биология', got %q", capturedTitle)
		}

		// Double URL-encoded
		sentMessage = SendMessageRequest{}
		editedMessage = SendMessageRequest{}
		capturedTitle = ""

		bot.handleUpdate(ctx, Update{
			UpdateType: "message_created",
			Message: &Message{
				Recipient: MessageRecipient{ChatID: 300},
				Sender: User{
					UserID:    12345,
					FirstName: "Alice",
					IsBot:     false,
				},
				Body: MessageBody{
					Text: "",
					Attachments: []Attachment{
						{
							Type:     "file",
							Filename: "%25D0%25BB%25D0%25B5%25D0%25BA%25D1%2586%25D0%25B8%25D1%258F.pdf",
							Payload: map[string]interface{}{
								"data": []byte("text content"),
							},
						},
					},
				},
			},
		})

		if capturedTitle != "лекция" {
			t.Errorf("expected title 'лекция', got %q", capturedTitle)
		}
	})

	t.Run("title updated from file_1 to downloaded filename when discovered via DownloadFile", func(t *testing.T) {
		fileServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''%D0%B0%D0%BD%D0%B0%D1%82%D0%BE%D0%BC%D0%B8%D1%8F.pdf")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("content of anatomy lecture"))
		}))
		defer fileServer.Close()

		sentMessage = SendMessageRequest{}
		editedMessage = SendMessageRequest{}
		capturedTitle = ""

		bot.handleUpdate(ctx, Update{
			UpdateType: "message_created",
			Message: &Message{
				Recipient: MessageRecipient{ChatID: 300},
				Sender: User{
					UserID:    12345,
					FirstName: "Alice",
					IsBot:     false,
				},
				Body: MessageBody{
					Text: "",
					Attachments: []Attachment{
						{
							Type: "file",
							// No filename provided initially; only URL
							URL: fileServer.URL + "/download",
						},
					},
				},
			},
		})

		if capturedTitle != "анатомия" {
			t.Errorf("expected title 'анатомия' discovered during download, got %q", capturedTitle)
		}
		lastMsg := getLastMessage()
		if !strings.Contains(lastMsg.Text, "🎉 Набор «анатомия» успешно создан!") {
			t.Errorf("expected confirmation text with title 'анатомия', got: %s", lastMsg.Text)
		}
	})

	t.Run("rune-safe truncation of long cyrillic title", func(t *testing.T) {
		sentMessage = SendMessageRequest{}
		editedMessage = SendMessageRequest{}
		capturedTitle = ""

		longRussianName := strings.Repeat("конспект", 15) + ".pdf" // 15 * 8 = 120 runes
		bot.handleUpdate(ctx, Update{
			UpdateType: "message_created",
			Message: &Message{
				Recipient: MessageRecipient{ChatID: 300},
				Sender: User{
					UserID:    12345,
					FirstName: "Alice",
					IsBot:     false,
				},
				Body: MessageBody{
					Text: "",
					Attachments: []Attachment{
						{
							Type:     "file",
							Filename: longRussianName,
							Payload: map[string]interface{}{
								"data": []byte("text content"),
							},
						},
					},
				},
			},
		})

		if !utf8.ValidString(capturedTitle) {
			t.Fatalf("capturedTitle is not valid UTF-8: %q", capturedTitle)
		}
		if !strings.HasSuffix(capturedTitle, "...") {
			t.Errorf("expected title to end with '...', got: %q", capturedTitle)
		}
		runes := []rune(capturedTitle)
		if len(runes) != 83 {
			t.Errorf("expected 83 runes (80 + '...'), got %d runes", len(runes))
		}
	})
}

func TestCleanFilename(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"", ""},
		{"   ", ""},
		{"лекция.pdf", "лекция.pdf"},
		{"%D0%BB%D0%B5%D0%BA%D1%86%D0%B8%D1%8F.pdf", "лекция.pdf"},
		{"%25D0%25BB%25D0%25B5%25D0%25BA%25D1%2586%25D0%25B8%25D1%258F.pdf", "лекция.pdf"},
		{"/path/to/some/file.txt", "file.txt"},
		{"..\\..\\secret.pdf", "secret.pdf"},
		{"\"quoted_file.pdf\"", "quoted_file.pdf"},
		{"'quoted_file.docx'", "quoted_file.docx"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			actual := cleanFilename(tt.input)
			if actual != tt.expected {
				t.Errorf("cleanFilename(%q) = %q, expected %q", tt.input, actual, tt.expected)
			}
		})
	}
}

func TestClient_DownloadFile(t *testing.T) {
	t.Run("extracts filename from RFC 5987 filename*", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Disposition", "attachment; filename*=UTF-8''%D0%BB%D0%B5%D0%BA%D1%86%D0%B8%D1%8F.pdf")
			w.Header().Set("Content-Type", "application/pdf")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("dummy pdf content"))
		}))
		defer ts.Close()

		client, err := NewClient("token", ts.URL)
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		res, err := client.DownloadFile(context.Background(), ts.URL+"/file")
		if err != nil {
			t.Fatalf("DownloadFile failed: %v", err)
		}
		if res.Filename != "лекция.pdf" {
			t.Errorf("expected Filename 'лекция.pdf', got %q", res.Filename)
		}
		if string(res.Data) != "dummy pdf content" {
			t.Errorf("unexpected Data: %s", string(res.Data))
		}
		if res.ContentType != "application/pdf" {
			t.Errorf("unexpected ContentType: %s", res.ContentType)
		}
	})

	t.Run("extracts filename from standard filename parameter", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Disposition", "attachment; filename=\"document.docx\"")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("dummy docx content"))
		}))
		defer ts.Close()

		client, err := NewClient("token", ts.URL)
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		res, err := client.DownloadFile(context.Background(), ts.URL+"/download")
		if err != nil {
			t.Fatalf("DownloadFile failed: %v", err)
		}
		if res.Filename != "document.docx" {
			t.Errorf("expected Filename 'document.docx', got %q", res.Filename)
		}
	})

	t.Run("falls back to URL path if Content-Disposition missing", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("plain text"))
		}))
		defer ts.Close()

		client, err := NewClient("token", ts.URL)
		if err != nil {
			t.Fatalf("failed to create client: %v", err)
		}

		res, err := client.DownloadFile(context.Background(), ts.URL+"/notes/биология.txt")
		if err != nil {
			t.Fatalf("DownloadFile failed: %v", err)
		}
		if res.Filename != "биология.txt" {
			t.Errorf("expected Filename 'биология.txt', got %q", res.Filename)
		}
	})
}

func ref[T any](v T) *T {
	return &v
}
