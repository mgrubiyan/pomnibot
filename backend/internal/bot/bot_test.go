package bot

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
				Username:  "t583_hakaton_max_bot",
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

	bot := NewBot(client, "https://pomnibot.steins.ru")
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
			UserID: 999,
		},
	})

	if receivedAuth != "test-token" {
		t.Errorf("expected Authorization 'test-token', got '%s'", receivedAuth)
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
