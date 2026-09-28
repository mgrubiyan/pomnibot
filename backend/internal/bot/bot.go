// Package bot implements MAX Bot API client and bot event loop.
package bot

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// Bot represents the Pomnibot MAX bot runner.
type Bot struct {
	client      *Client
	appURL      string
	userService usecase.UserService
	botUser     *User
}

// NewBot creates a new Bot instance.
func NewBot(client *Client, appURL string, userService usecase.UserService) (*Bot, error) {
	if client == nil {
		return nil, errors.New("bot client is required")
	}
	if userService == nil {
		return nil, errors.New("user service is required")
	}
	if appURL == "" {
		appURL = "https://pomnibot.steins.ru"
	}
	return &Bot{
		client:      client,
		appURL:      strings.TrimSuffix(appURL, "/"),
		userService: userService,
	}, nil
}

// Start initializes the bot and starts the polling loop.
func (b *Bot) Start(ctx context.Context) error {
	// 1. Fetch bot profile
	me, err := b.client.GetMe(ctx)
	if err != nil {
		return err
	}
	b.botUser = me
	username := ""
	if me.Username != nil {
		username = *me.Username
	}
	slog.Info("connected to MAX Bot API",
		"bot_user_id", me.UserID,
		"username", username,
		"name", me.Name,
	)

	// 2. Clear any webhook subscriptions to allow Long Polling
	if err := b.client.CleanUpSubscriptions(ctx); err != nil {
		slog.Warn("could not clean up subscriptions (continuing anyway)", "error", err)
	}

	// 3. Start long polling loop
	slog.Info("starting MAX bot long-polling loop...", "app_url", b.appURL)
	go b.pollLoop(ctx)

	return nil
}

func (b *Bot) pollLoop(ctx context.Context) {
	var marker *int64
	backoff := 1 * time.Second

	for {
		select {
		case <-ctx.Done():
			slog.Info("stopping bot polling loop")
			return
		default:
		}

		resp, err := b.client.GetUpdates(ctx, marker, 30)
		if err != nil {
			if errors.Is(ctx.Err(), context.Canceled) {
				return
			}
			slog.Error("error polling updates from MAX", "error", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			if backoff < 30*time.Second {
				backoff *= 2
			}
			continue
		}

		backoff = 1 * time.Second

		if resp.Marker != nil {
			marker = resp.Marker
		}

		for _, update := range resp.Updates {
			b.handleUpdate(ctx, update)
		}
	}
}

func (b *Bot) handleUpdate(ctx context.Context, u Update) {
	slog.Info("received update from MAX", "type", u.UpdateType)

	var chatID int64
	var userID int64

	switch u.UpdateType {
	case "bot_started":
		chatID = u.ChatID
		if u.User != nil {
			userID = u.User.UserID
			b.upsertUser(ctx, *u.User)
		}
	case "message_created":
		if u.Message == nil {
			return
		}
		// Ignore messages from the bot itself
		if u.Message.Sender.IsBot || (b.botUser != nil && u.Message.Sender.UserID == b.botUser.UserID) {
			return
		}
		chatID = u.Message.Recipient.ChatID
		userID = u.Message.Sender.UserID
		b.upsertUser(ctx, u.Message.Sender)
	default:
		// Other events can be ignored or handled later
		return
	}

	if chatID == 0 && userID == 0 {
		return
	}

	b.sendWelcomeMessage(ctx, chatID, userID)
}

func (b *Bot) upsertUser(ctx context.Context, u User) {
	if b.userService == nil {
		return
	}
	err := b.userService.UpsertUser(ctx, usecase.UpsertUserParams{
		ID:        u.UserID,
		FirstName: u.FirstName,
		LastName:  u.LastName,
		Username:  u.Username,
		IsBot:     u.IsBot,
	})
	if err != nil {
		slog.Error("failed to upsert user on bot update",
			"user_id", u.UserID,
			"error", err,
		)
	}
}

func (b *Bot) sendWelcomeMessage(ctx context.Context, chatID int64, userID int64) {
	username := ""
	if b.botUser != nil && b.botUser.Username != nil {
		username = *b.botUser.Username
	}

	buttons := [][]map[string]interface{}{}

	if username != "" {
		buttons = append(buttons, []map[string]interface{}{
			{
				"type":    "open_app",
				"text":    "Открыть Помнибот 📚",
				"web_app": username,
			},
		})
	}

	buttons = append(buttons, []map[string]interface{}{
		{
			"type": "link",
			"text": "Открыть в браузере 🌐",
			"url":  b.appURL,
		},
	})

	msg := SendMessageRequest{
		Text: "Привет! Я Помнибот 🤖\n\nЯ помогаю готовиться к экзаменам и повторять материал по твоим конспектам без выдумок и со ссылками на текст.\n\nНажми кнопку ниже, чтобы открыть мини-приложение:",
		Attachments: []Attachment{
			{
				Type: "inline_keyboard",
				Payload: map[string]interface{}{
					"buttons": buttons,
				},
			},
		},
	}

	if err := b.client.SendMessage(ctx, chatID, userID, msg); err != nil {
		slog.Error("failed to send welcome message",
			"chat_id", chatID,
			"user_id", userID,
			"error", err,
		)
	} else {
		slog.Info("successfully sent welcome message with mini app button",
			"chat_id", chatID,
			"user_id", userID,
		)
	}
}
