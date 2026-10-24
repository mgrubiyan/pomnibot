package telegram

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestMessengerSendToUser(t *testing.T) {
	var chats []int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChatID int64  `json:"chat_id"`
			Text   string `json:"text"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body.Text != "Жалоба" {
			t.Errorf("text = %q", body.Text)
		}
		chats = append(chats, body.ChatID)
		sendOK(w)
	}))
	defer server.Close()
	client := NewClient("secret")
	client.baseURL = server.URL
	m := NewMessenger(client, "https://example.test")
	for _, account := range []usecase.Account{
		{MaxID: 1, BotStarted: true, ProfileComplete: true},
		{TgID: 2, ProfileComplete: true},
		{TgID: 3, BotStarted: true},
		{TgID: 4, BotStarted: true, ProfileComplete: true},
	} {
		if err := m.SendToUser(context.Background(), account, "Жалоба"); err != nil {
			t.Fatal(err)
		}
	}
	if len(chats) != 1 || chats[0] != 4 {
		t.Fatalf("sent to %v, want only the user who started the bot and finished the profile", chats)
	}
}
