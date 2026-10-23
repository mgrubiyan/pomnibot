package app

import (
	"fmt"

	"github.com/mgrubiyan/pomnibot/backend/internal/config"
	maxtransport "github.com/mgrubiyan/pomnibot/backend/internal/transport/max"
	"github.com/mgrubiyan/pomnibot/backend/internal/transport/telegram"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// newIssueNotifier tells authors about complaints through the bots that run
// in prod; elsewhere there are no bots and it sends nothing. It has clients of
// its own, so the HTTP API has it before the bots start.
func newIssueNotifier(cfg config.Config, users usecase.UserService) (*usecase.ChatIssueNotifier, error) {
	if cfg.Environment != config.Prod {
		return usecase.NewChatIssueNotifier(users), nil
	}
	client, err := maxtransport.NewClient(cfg.MaxBotToken, cfg.MaxAPIURL, cfg.MaxInsecureSkipVerify)
	if err != nil {
		return nil, fmt.Errorf("initialize MAX client: %w", err)
	}
	messengers := []usecase.Messenger{maxtransport.NewMessenger(client, cfg.AppURL)}
	// The same conditions the Telegram bot starts on.
	if cfg.TelegramBotToken != "" && telegram.ValidAppURL(cfg.AppURL) {
		messengers = append(messengers, telegram.NewMessenger(telegram.NewClient(cfg.TelegramBotToken), cfg.AppURL))
	}
	return usecase.NewChatIssueNotifier(users, messengers...), nil
}
