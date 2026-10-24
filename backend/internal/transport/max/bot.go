// Package max is the MAX bot: the MAX Bot API client, the event loop,
// receiving notes and review reminders.
package max

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/notes"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// NotificationConfig contains the validated review reminder schedule.
type NotificationConfig struct {
	Enabled  bool
	Hour     int
	Minute   int
	Location *time.Location
}

// Bot represents the Pomnibot MAX bot runner.
type Bot struct {
	notifications NotificationConfig
	client        *Client
	appURL        string
	userService   usecase.UserService
	notes         *notes.Maker
	botUser       *User
	inflight      sync.WaitGroup
	background    sync.WaitGroup
}

// NewBot creates a new Bot instance.
func NewBot(
	client *Client,
	appURL string,
	userService usecase.UserService,
	maker *notes.Maker,
	notifications ...NotificationConfig,
) (*Bot, error) {
	if client == nil {
		return nil, errors.New("bot client is required")
	}
	if userService == nil {
		return nil, errors.New("user service is required")
	}
	if maker == nil {
		return nil, errors.New("notes maker is required")
	}
	if appURL == "" {
		appURL = "https://pomnibot.steins.ru"
	}
	schedule := NotificationConfig{Enabled: true, Hour: 10, Location: time.FixedZone("Europe/Moscow", 3*3600)}
	if len(notifications) > 0 {
		schedule = notifications[0]
	}
	return &Bot{
		notifications: schedule,
		client:        client,
		appURL:        strings.TrimSuffix(appURL, "/"),
		userService:   userService,
		notes:         maker,
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
	b.background.Go(func() { b.pollLoop(ctx) })

	// 4. Start review notifications scheduler
	b.startNotificationLoop(ctx)

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
			b.dispatch(ctx, update)
		}
	}
}

// dispatch handles u in its own goroutine, so a document that takes minutes
// to turn into cards does not hold up other users' messages.
func (b *Bot) dispatch(ctx context.Context, u Update) {
	b.inflight.Go(func() { b.handleUpdate(ctx, u) })
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
			if _, err := b.upsertUser(ctx, *u.User); err != nil {
				return
			}
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
		internalID, err := b.upsertUser(ctx, u.Message.Sender)
		if err != nil {
			if _, sendErr := b.client.SendMessage(ctx, chatID, userID, SendMessageRequest{Text: "Не удалось загрузить профиль. Повторите сообщение чуть позже."}); sendErr != nil {
				slog.WarnContext(ctx, "send MAX profile error", "error", sendErr)
			}
			return
		}

		if atts := b.getAllFileAttachments(u.Message); len(atts) > 0 {
			b.handleFileMessage(ctx, chatID, userID, internalID, u.Message.Body.Text, atts)
			return
		}
	default:
		// Other events can be ignored or handled later
		return
	}

	if chatID == 0 && userID == 0 {
		return
	}

	b.sendWelcomeMessage(ctx, chatID, userID)
}

type attachmentInfo struct {
	filename string
	url      string
	data     []byte
}

func (b *Bot) handleFileMessage(ctx context.Context, chatID int64, userID int64, internalID uuid.UUID, caption string, atts []attachmentInfo) {
	log := slog.With("user_id", userID, "chat_id", chatID)

	count := len(atts)
	statusText := notes.ReceivedText(count)

	mid, err := b.client.SendMessage(ctx, chatID, userID, SendMessageRequest{
		Text: statusText,
	})
	if err != nil {
		log.Error("failed to send initial status message", "error", err)
	}

	if chatID != 0 {
		func() {
			if err := b.client.SendAction(ctx, chatID, "typing_on"); err != nil {
				log.Warn("failed to send typing action", "error", err)
			}
		}()
	}

	updateStatus := func(text string) {
		if mid != "" {
			if editErr := b.client.EditMessage(ctx, mid, SendMessageRequest{Text: text}); editErr == nil {
				return
			}
		}
		if _, err := b.client.SendMessage(ctx, chatID, userID, SendMessageRequest{Text: text}); err != nil {
			log.Error("failed to send status message", "text", text, "error", err)
		}
	}

	files := make([]ingest.File, 0, len(atts))
	for _, att := range atts {
		var data []byte
		var discoveredName string
		if len(att.data) > 0 {
			data = att.data
		} else if att.url != "" {
			downloaded, err := b.client.DownloadFile(ctx, att.url)
			if err != nil {
				log.Error("failed to download attachment", "url", att.url, "error", err)
				updateStatus(notes.TextResend)
				return
			}
			data = downloaded.Data
			discoveredName = downloaded.Filename
		} else {
			log.Error("attachment has neither data nor url", "filename", att.filename)
			updateStatus(notes.TextResend)
			return
		}

		// The download may name a file the attachment left unnamed; the
		// set is named after the first file.
		name := att.filename
		if notes.IsGenericFilename(name) && !notes.IsGenericFilename(discoveredName) {
			name = discoveredName
		}
		files = append(files, ingest.File{Name: name, Data: data})
	}

	// Files are downloaded before the queue, while their links are fresh.
	var (
		mu       sync.Mutex
		lastEdit time.Time
	)
	res, err := b.notes.Make(ctx, notes.Request{UserID: internalID, Caption: caption, Files: files}, notes.Progress{
		Queued: func(ahead int) {
			updateStatus(notes.QueuedText(count, ahead))
		},
		Reading: func() { updateStatus(notes.TextReading) },
		Generating: func() {
			updateStatus(notes.TextGenerating)
		},
		Cards: func(total int) {
			mu.Lock()
			defer mu.Unlock()
			if time.Since(lastEdit) < 1500*time.Millisecond {
				return
			}
			lastEdit = time.Now()
			if mid != "" {
				if err := b.client.EditMessage(ctx, mid, SendMessageRequest{
					Text: notes.CardsText(total),
				}); err != nil {
					log.Warn("failed to update progress message", "error", err)
				}
			}
		},
	})
	if err != nil {
		if text := notes.FailureText(err); text != "" {
			updateStatus(text)
		}
		return
	}

	finalText := notes.SuccessText(res)
	b.editOrSendMessageWithButtons(ctx, mid, chatID, userID, finalText)
}

func (b *Bot) getAllFileAttachments(m *Message) []attachmentInfo {
	if m == nil {
		return nil
	}
	var allAtts []Attachment
	if len(m.Body.Attachments) > 0 {
		allAtts = append(allAtts, m.Body.Attachments...)
	}
	if len(m.Attachments) > 0 {
		allAtts = append(allAtts, m.Attachments...)
	}

	var results []attachmentInfo
	for idx, att := range allAtts {
		filename := extractFilename(att)
		url := extractURL(att)
		var data []byte
		if att.Payload != nil {
			if rawData, ok := att.Payload["data"].([]byte); ok {
				data = rawData
			} else if strData, ok := att.Payload["data"].(string); ok && strData != "" {
				data = []byte(strData)
			}
		}

		t := strings.ToLower(att.Type)
		isDocOrImage := t == "file" || t == "image" || t == "photo" || t == "document"
		if !isDocOrImage && filename != "" {
			ext := strings.ToLower(filepath.Ext(filename))
			switch ext {
			case ".txt", ".pdf", ".png", ".jpg", ".jpeg", ".webp", ".doc", ".docx", ".pptx":
				isDocOrImage = true
			}
		}

		if isDocOrImage {
			if filename == "" {
				if t == "image" || t == "photo" {
					filename = fmt.Sprintf("photo_%d.jpg", idx+1)
				} else {
					filename = fmt.Sprintf("file_%d", idx+1)
				}
			}
			results = append(results, attachmentInfo{
				filename: filename,
				url:      url,
				data:     data,
			})
		}
	}
	return results
}

func extractURL(att Attachment) string {
	if strings.TrimSpace(att.URL) != "" {
		return strings.TrimSpace(att.URL)
	}
	if strings.TrimSpace(att.Token) != "" {
		return strings.TrimSpace(att.Token)
	}
	if att.Payload == nil {
		return ""
	}
	for _, key := range []string{"url", "download_url", "file_url", "token"} {
		if v, ok := att.Payload[key].(string); ok && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func extractFilename(att Attachment) string {
	candidates := []string{
		att.Filename,
		att.Name,
		att.Title,
	}
	if att.Payload != nil {
		for _, key := range []string{"filename", "name", "title"} {
			if v, ok := att.Payload[key].(string); ok {
				candidates = append(candidates, v)
			}
		}
	}
	for _, cand := range candidates {
		cleaned := cleanFilename(cand)
		if cleaned != "" {
			return cleaned
		}
	}
	return ""
}

func (b *Bot) upsertUser(ctx context.Context, u User) (uuid.UUID, error) {
	account, err := b.userService.UpsertMAXUser(ctx, usecase.MAXUserParams{MaxID: u.UserID, FirstName: u.FirstName, LastName: u.LastName, Username: u.Username, IsBot: u.IsBot})
	if err != nil {
		slog.ErrorContext(ctx, "resolve MAX user", "error", err)
		return uuid.Nil, err
	}
	return account.User.ID, nil
}

// Wait joins polling, reminders and active note processing after cancellation.
func (b *Bot) Wait() { b.background.Wait(); b.inflight.Wait() }

func (b *Bot) buildAppButtons() [][]map[string]interface{} {
	username := ""
	if b.botUser != nil && b.botUser.Username != nil {
		username = *b.botUser.Username
	}
	return appButtons(username, b.appURL)
}

// appButtons open the mini app in MAX, when the bot's username is known, and
// in the browser.
func appButtons(username, appURL string) [][]map[string]interface{} {
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
			"url":  appURL,
		},
	})

	return buttons
}

func (b *Bot) editOrSendMessageWithButtons(ctx context.Context, mid string, chatID int64, userID int64, text string) {
	buttons := b.buildAppButtons()

	msg := SendMessageRequest{
		Text: text,
		Attachments: []Attachment{
			{
				Type: "inline_keyboard",
				Payload: map[string]interface{}{
					"buttons": buttons,
				},
			},
		},
	}

	if mid != "" {
		if err := b.client.EditMessage(ctx, mid, msg); err == nil {
			slog.Info("successfully edited message with buttons",
				"chat_id", chatID,
				"user_id", userID,
				"mid", mid,
			)
			return
		}
	}

	if _, err := b.client.SendMessage(ctx, chatID, userID, msg); err != nil {
		slog.Error("failed to send message with buttons",
			"chat_id", chatID,
			"user_id", userID,
			"error", err,
		)
	} else {
		slog.Info("successfully sent message with buttons",
			"chat_id", chatID,
			"user_id", userID,
		)
	}
}

func (b *Bot) sendMessageWithButtons(ctx context.Context, chatID int64, userID int64, text string) {
	b.editOrSendMessageWithButtons(ctx, "", chatID, userID, text)
}

func (b *Bot) sendWelcomeMessage(ctx context.Context, chatID int64, userID int64) {
	welcomeText := "Привет! Я Помнибот 🤖\n\nЯ помогаю готовиться к экзаменам и повторять материал по твоим конспектам без выдумок и со ссылками на текст.\n\nНажми кнопку ниже, чтобы открыть мини-приложение:"
	b.sendMessageWithButtons(ctx, chatID, userID, welcomeText)
}

// nextRunDuration calculates the duration until the next occurrence of targetHour:targetMinute in the specified location.
func nextRunDuration(now time.Time, targetHour, targetMinute int, loc *time.Location) time.Duration {
	if loc == nil {
		loc = time.UTC
	}
	nowInLoc := now.In(loc)
	targetToday := time.Date(nowInLoc.Year(), nowInLoc.Month(), nowInLoc.Day(), targetHour, targetMinute, 0, 0, loc)

	if !targetToday.After(nowInLoc) {
		targetToday = targetToday.AddDate(0, 0, 1)
	}

	return targetToday.Sub(nowInLoc)
}

// SendReviewReminders queries users who have cards due today or earlier and sends them a review reminder message.
func (b *Bot) SendReviewReminders(ctx context.Context) (int, error) {
	if b.userService == nil {
		return 0, errors.New("user service is not configured")
	}

	tz := b.notifications.Location.String()

	userIDs, err := b.userService.GetUsersWithDueFacts(ctx, time.Now(), tz)
	if err != nil {
		return 0, fmt.Errorf("get users with due facts: %w", err)
	}

	if len(userIDs) == 0 {
		slog.InfoContext(ctx, "no users with due facts found for review reminder")
		return 0, nil
	}

	text := "Привет! ⏰ Пора повторить материал — в приложении тебя ждут карточки на сегодня. Загляни в Помнибот! 📚"
	buttons := b.buildAppButtons()
	msg := SendMessageRequest{
		Text: text,
		Attachments: []Attachment{
			{
				Type: "inline_keyboard",
				Payload: map[string]interface{}{
					"buttons": buttons,
				},
			},
		},
	}

	successCount := 0
	for _, userID := range userIDs {
		if userID == uuid.Nil {
			continue
		}

		account, err := b.userService.GetAccount(ctx, userID)
		if err != nil {
			return successCount, err
		}
		if account.MaxID == 0 {
			continue
		}
		if _, err := b.client.SendMessage(ctx, 0, account.MaxID, msg); err != nil {
			slog.WarnContext(ctx, "failed to send review reminder to user",
				"user_id", userID,
				"error", err,
			)
			continue
		}

		successCount++
		// Small delay to be polite to API rate limits
		select {
		case <-ctx.Done():
			return successCount, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}

	slog.InfoContext(ctx, "sent review reminders",
		"candidate_count", len(userIDs),
		"success_count", successCount,
	)

	return successCount, nil
}

func (b *Bot) startNotificationLoop(ctx context.Context) {
	if !b.notifications.Enabled {
		return
	}
	targetHour, targetMinute := b.notifications.Hour, b.notifications.Minute
	loc := b.notifications.Location

	slog.InfoContext(ctx, "starting review notifications scheduler",
		"time", fmt.Sprintf("%02d:%02d", targetHour, targetMinute),
		"tz", loc.String(),
	)

	b.background.Go(func() {
		for {
			delay := nextRunDuration(time.Now(), targetHour, targetMinute, loc)
			slog.InfoContext(ctx, "scheduled next review notifications dispatch",
				"delay", delay.String(),
				"target_time", time.Now().Add(delay).In(loc).Format(time.RFC3339),
			)

			select {
			case <-ctx.Done():
				return
			case <-time.After(delay):
			}

			if ctx.Err() != nil {
				return
			}

			if _, err := b.SendReviewReminders(ctx); err != nil {
				slog.ErrorContext(ctx, "error dispatching review reminders", "error", err)
			}
		}
	})
}
