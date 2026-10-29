package app

import (
	"context"
	"log/slog"
	"sync"

	"github.com/mgrubiyan/pomnibot/backend/internal/config"
	"github.com/mgrubiyan/pomnibot/backend/internal/notes"
	"github.com/mgrubiyan/pomnibot/backend/internal/transport/telegram"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// startTelegramBot keeps optional Telegram startup and failures independent of
// the web server and MAX. The returned function joins it after cancellation.
func startTelegramBot(ctx context.Context, cfg config.Config, maker *notes.Maker, users usecase.UserService) func() {
	var background sync.WaitGroup
	if cfg.TelegramBotToken == "" {
		slog.WarnContext(ctx, "TG_BOT_TOKEN is not set; Telegram bot is disabled")
		return background.Wait
	}
	background.Go(func() {
		bot, err := telegram.NewBot(telegram.NewClient(cfg.TelegramBotToken), users, maker, cfg.AppURL, cfg.Notifications)
		if err != nil {
			slog.ErrorContext(ctx, "Telegram bot failed", "stage", "initialize", "error", err)
			return
		}
		if err := bot.Start(ctx); err != nil {
			if ctx.Err() == nil {
				slog.ErrorContext(ctx, "Telegram bot failed", "stage", "start", "error", err)
			}
			return
		}
		bot.Wait()
	})
	return background.Wait
}
