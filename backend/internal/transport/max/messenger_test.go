package max

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

func TestMessengerSendToUser(t *testing.T) {
	var profiles, sent atomic.Int32
	var userID string
	var body SendMessageRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/me":
			profiles.Add(1)
			_ = json.NewEncoder(w).Encode(User{UserID: 1, Username: ref("pomnibot_bot"), IsBot: true})
		case "/messages":
			sent.Add(1)
			userID = r.URL.Query().Get("user_id")
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = w.Write([]byte(`{"success":true}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()
	client, err := NewClient("token", server.URL)
	if err != nil {
		t.Fatal(err)
	}
	m := NewMessenger(client, "https://pomnibot.test/")
	ctx := context.Background()

	if err = m.SendToUser(ctx, usecase.Account{}, "text"); err != nil || sent.Load() != 0 {
		t.Fatalf("user without MAX: sent %d, %v", sent.Load(), err)
	}
	for range 2 {
		if err = m.SendToUser(ctx, usecase.Account{MaxID: 77}, "Жалоба"); err != nil {
			t.Fatal(err)
		}
	}
	if sent.Load() != 2 || userID != "77" || body.Text != "Жалоба" {
		t.Fatalf("sent %d to %q: %+v", sent.Load(), userID, body)
	}
	if profiles.Load() != 1 {
		t.Errorf("bot profile asked %d times, want once", profiles.Load())
	}
	buttons, _ := json.Marshal(body.Attachments)
	var keyboard []struct {
		Payload struct {
			Buttons [][]map[string]string `json:"buttons"`
		} `json:"payload"`
	}
	if err = json.Unmarshal(buttons, &keyboard); err != nil || len(keyboard) != 1 || len(keyboard[0].Payload.Buttons) != 2 {
		t.Fatalf("keyboard = %s, %v", buttons, err)
	}
	if app := keyboard[0].Payload.Buttons[0][0]; app["type"] != "open_app" || app["web_app"] != "pomnibot_bot" {
		t.Errorf("app button = %v", app)
	}
	if link := keyboard[0].Payload.Buttons[1][0]; link["url"] != "https://pomnibot.test" {
		t.Errorf("link button = %v", link)
	}
}
