package telegram

import (
	"context"

	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// Messenger sends users messages from the bot outside a dialogue, such as a
// complaint about a card to its author.
type Messenger struct {
	client *Client
	appURL string
}

// NewMessenger sends through client with a button to the mini app at appURL.
func NewMessenger(client *Client, appURL string) *Messenger {
	return &Messenger{client: client, appURL: appURL}
}

// SendToUser implements usecase.Messenger. Like reminders, it writes only to
// users who started the bot and finished their profile.
func (m *Messenger) SendToUser(ctx context.Context, account usecase.Account, text string) error {
	if account.TgID == 0 || !account.BotStarted || !account.ProfileComplete {
		return nil
	}
	_, err := m.client.Send(ctx, account.TgID, 0, text, m.appURL)
	return err
}
