package max

import (
	"context"
	"log/slog"
	"strings"
	"sync"

	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// Messenger sends users messages from the bot outside a dialogue, such as a
// complaint about a card to its author.
type Messenger struct {
	client *Client
	appURL string

	mu       sync.Mutex
	username string // the bot's, for the button that opens the mini app
}

// NewMessenger sends through client with buttons to the mini app at appURL.
func NewMessenger(client *Client, appURL string) *Messenger {
	return &Messenger{client: client, appURL: strings.TrimSuffix(appURL, "/")}
}

// SendToUser implements usecase.Messenger; a user without MAX is skipped.
func (m *Messenger) SendToUser(ctx context.Context, account usecase.Account, text string) error {
	if account.MaxID == 0 {
		return nil
	}
	_, err := m.client.SendMessage(ctx, 0, account.MaxID, SendMessageRequest{
		Text: text,
		Attachments: []Attachment{{
			Type:    "inline_keyboard",
			Payload: map[string]interface{}{"buttons": appButtons(m.botUsername(ctx), m.appURL)},
		}},
	})
	return err
}

// botUsername asks MAX once it answers; until then the message goes with the
// browser button only.
func (m *Messenger) botUsername(ctx context.Context) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.username != "" {
		return m.username
	}
	me, err := m.client.GetMe(ctx)
	if err != nil {
		slog.WarnContext(ctx, "MAX bot profile unavailable", "error", err)
		return ""
	}
	if me.Username != nil {
		m.username = *me.Username
	}
	return m.username
}
