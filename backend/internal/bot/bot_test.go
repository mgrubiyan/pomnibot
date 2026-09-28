package bot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

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

func TestNewBot_Validation(t *testing.T) {
	client := &Client{}
	userSvc := &mockUserService{}

	t.Run("nil client returns error", func(t *testing.T) {
		b, err := NewBot(nil, "http://localhost", userSvc)
		if err == nil || b != nil {
			t.Fatalf("expected error on nil client, got %v", err)
		}
	})

	t.Run("nil user service returns error", func(t *testing.T) {
		b, err := NewBot(client, "http://localhost", nil)
		if err == nil || b != nil {
			t.Fatalf("expected error on nil user service, got %v", err)
		}
	})

	t.Run("valid parameters returns Bot", func(t *testing.T) {
		b, err := NewBot(client, "", userSvc)
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

	bot, err := NewBot(client, "https://pomnibot.steins.ru", userSvc)
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

	bot, err := NewBot(client, "https://pomnibot.steins.ru", userSvc)
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

	bot, err := NewBot(client, "https://pomnibot.steins.ru", userSvc)
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

func ref[T any](v T) *T {
	return &v
}
