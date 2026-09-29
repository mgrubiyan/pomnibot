// Package bot implements MAX Bot API client and bot event loop.
package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/generator"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

// FileExtractor defines interface for extracting text from files/images.
type FileExtractor interface {
	Extract(ctx context.Context, files []ingest.File) (ingest.Result, error)
}

// CardGenerator defines interface for generating cards from lecture notes.
type CardGenerator interface {
	Generate(ctx context.Context, doc generator.Document, onBatch func(generator.Batch)) (generator.Result, error)
}

type generatorAdapter struct {
	gen *generator.Generator
}

func (a *generatorAdapter) Generate(ctx context.Context, doc generator.Document, onBatch func(generator.Batch)) (generator.Result, error) {
	if a == nil || a.gen == nil {
		return generator.Result{}, errors.New("generator is nil")
	}
	g := a.gen
	if onBatch != nil {
		g = g.WithOnBatch(onBatch)
	}
	return g.Generate(ctx, doc)
}

// NewGeneratorAdapter creates a CardGenerator adapter for *generator.Generator.
func NewGeneratorAdapter(g *generator.Generator) CardGenerator {
	if g == nil {
		return nil
	}
	return &generatorAdapter{gen: g}
}

// Bot represents the Pomnibot MAX bot runner.
type Bot struct {
	client      *Client
	appURL      string
	userService usecase.UserService
	setService  usecase.SetService
	extractor   FileExtractor
	generator   CardGenerator
	botUser     *User
}

// NewBot creates a new Bot instance.
func NewBot(
	client *Client,
	appURL string,
	userService usecase.UserService,
	setService usecase.SetService,
	extractor FileExtractor,
	generator CardGenerator,
) (*Bot, error) {
	if client == nil {
		return nil, errors.New("bot client is required")
	}
	if userService == nil {
		return nil, errors.New("user service is required")
	}
	if setService == nil {
		return nil, errors.New("set service is required")
	}
	if appURL == "" {
		appURL = "https://pomnibot.steins.ru"
	}
	return &Bot{
		client:      client,
		appURL:      strings.TrimSuffix(appURL, "/"),
		userService: userService,
		setService:  setService,
		extractor:   extractor,
		generator:   generator,
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

		if atts := b.getAllFileAttachments(u.Message); len(atts) > 0 {
			b.handleFileMessage(ctx, chatID, userID, u.Message.Body.Text, atts)
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

func (b *Bot) handleFileMessage(ctx context.Context, chatID int64, userID int64, caption string, atts []attachmentInfo) {
	firstFilename := ""
	if len(atts) > 0 {
		firstFilename = atts[0].filename
	}
	title := resolveSetTitle(caption, firstFilename)

	count := len(atts)
	plural := "файлов"
	if count == 1 {
		plural = "файл"
	} else if count >= 2 && count <= 4 {
		plural = "файла"
	}
	statusText := fmt.Sprintf("📸 Получил %d %s. Распознаю текст... ⏳", count, plural)

	mid, err := b.client.SendMessage(ctx, chatID, userID, SendMessageRequest{
		Text: statusText,
	})
	if err != nil {
		slog.Error("failed to send initial status message", "error", err)
	}

	if chatID != 0 {
		go func() {
			_ = b.client.SendAction(context.Background(), chatID, "typing_on")
		}()
	}

	updateStatus := func(text string) {
		if mid != "" {
			if editErr := b.client.EditMessage(ctx, mid, SendMessageRequest{Text: text}); editErr == nil {
				return
			}
		}
		_, _ = b.client.SendMessage(ctx, chatID, userID, SendMessageRequest{Text: text})
	}

	if b.extractor == nil || b.generator == nil {
		slog.Error("extractor or generator not configured on bot")
		updateStatus("К сожалению, произошла ошибка при создании конспекта. Попробуйте еще раз позже.")
		return
	}

	files := make([]ingest.File, 0, len(atts))
	for i, att := range atts {
		var data []byte
		var discoveredName string
		if len(att.data) > 0 {
			data = att.data
		} else if att.url != "" {
			downloaded, err := b.client.DownloadFile(ctx, att.url)
			if err != nil {
				slog.Error("failed to download attachment", "url", att.url, "error", err)
				updateStatus("К сожалению, произошла ошибка при создании конспекта. Попробуйте еще раз позже.")
				return
			}
			data = downloaded.Data
			discoveredName = downloaded.Filename
		} else {
			slog.Error("attachment has neither data nor url", "filename", att.filename)
			updateStatus("К сожалению, произошла ошибка при создании конспекта. Попробуйте еще раз позже.")
			return
		}

		effectiveName := att.filename
		if isGenericFilename(effectiveName) && !isGenericFilename(discoveredName) {
			effectiveName = discoveredName
			atts[i].filename = discoveredName
			if i == 0 && caption == "" && isGenericTitle(title) {
				title = resolveSetTitle("", discoveredName)
			}
		}

		files = append(files, ingest.File{
			Name: effectiveName,
			Data: data,
		})
	}

	if len(files) == 0 {
		updateStatus("К сожалению, произошла ошибка при создании конспекта. Попробуйте еще раз позже.")
		return
	}

	extracted, err := b.extractor.Extract(ctx, files)
	if err != nil {
		slog.Error("failed to extract text from files", "error", err)
		updateStatus("К сожалению, произошла ошибка при создании конспекта. Попробуйте еще раз позже.")
		return
	}

	if strings.TrimSpace(extracted.Text) == "" {
		slog.Warn("no text found in files", "files_count", len(files))
		updateStatus("К сожалению, произошла ошибка при создании конспекта. Попробуйте еще раз позже.")
		return
	}

	updateStatus("🧠 Анализирую конспект и создаю карточки... ⏳")

	doc := generator.Document{
		Text:  extracted.Text,
		Title: title,
	}
	for _, p := range extracted.Pages {
		doc.PageStarts = append(doc.PageStarts, p.Start)
	}

	var (
		mu         sync.Mutex
		lastEdit   time.Time
		totalCards int
	)
	onBatch := func(bCount generator.Batch) {
		mu.Lock()
		defer mu.Unlock()
		totalCards += len(bCount.Cards)
		if time.Since(lastEdit) < 1500*time.Millisecond {
			return
		}
		lastEdit = time.Now()
		if mid != "" {
			_ = b.client.EditMessage(ctx, mid, SendMessageRequest{
				Text: fmt.Sprintf("🧠 Создаю карточки... Уже готово: %d ⏳", totalCards),
			})
		}
	}

	genResult, err := b.generator.Generate(ctx, doc, onBatch)
	if err != nil {
		slog.Error("failed to generate cards from document", "error", err)
		updateStatus("К сожалению, произошла ошибка при создании конспекта. Попробуйте еще раз позже.")
		return
	}

	if len(genResult.Cards) == 0 {
		slog.Warn("no cards generated from document")
		updateStatus("К сожалению, произошла ошибка при создании конспекта. Попробуйте еще раз позже.")
		return
	}

	cardSet, err := b.setService.SaveGeneratedSet(ctx, userID, title, genResult)
	if err != nil {
		slog.Error("failed to save generated set to database", "error", err)
		updateStatus("К сожалению, произошла ошибка при создании конспекта. Попробуйте еще раз позже.")
		return
	}

	shareCode := ""
	if cardSet.ShareCode.IsSet() {
		shareCode = cardSet.ShareCode.Value
	}

	finalText := fmt.Sprintf("🎉 Набор «%s» успешно создан!\nКод для совместного доступа: %s\n\nОткрой мини-приложение, чтобы начать тренировку:", cardSet.Title, shareCode)
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
			case ".txt", ".pdf", ".png", ".jpg", ".jpeg", ".webp", ".doc", ".docx":
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

var genericFileRegex = regexp.MustCompile(`^(file_\d+|photo_\d+\.jpg)$`)

func isGenericFilename(name string) bool {
	name = strings.TrimSpace(name)
	return name == "" || genericFileRegex.MatchString(name)
}

func isGenericTitle(title string) bool {
	title = strings.TrimSpace(title)
	if title == "" || genericFileRegex.MatchString(title) {
		return true
	}
	if strings.HasPrefix(title, "Новый конспект (") {
		return true
	}
	return false
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

func resolveSetTitle(caption, filename string) string {
	caption = strings.TrimSpace(caption)
	if caption != "" {
		lines := strings.Split(caption, "\n")
		title := strings.TrimSpace(lines[0])
		runes := []rune(title)
		if len(runes) > 80 {
			title = string(runes[:80]) + "..."
		}
		return title
	}
	if filename != "" {
		base := strings.TrimSuffix(filename, filepath.Ext(filename))
		base = strings.TrimSpace(base)
		if base != "" {
			runes := []rune(base)
			if len(runes) > 80 {
				base = string(runes[:80]) + "..."
			}
			return base
		}
	}
	return fmt.Sprintf("Новый конспект (%s)", time.Now().Format("02.01 15:04"))
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

func (b *Bot) buildAppButtons() [][]map[string]interface{} {
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

	tz := os.Getenv("NOTIFICATION_TZ")
	if tz == "" {
		tz = "Europe/Moscow"
	}

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
		if userID == 0 || userID == 100001 {
			continue
		}

		if _, err := b.client.SendMessage(ctx, 0, userID, msg); err != nil {
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
	if os.Getenv("NOTIFICATIONS_ENABLED") == "false" {
		slog.InfoContext(ctx, "review notifications are disabled via NOTIFICATIONS_ENABLED=false")
		return
	}

	timeStr := os.Getenv("NOTIFICATION_TIME")
	if timeStr == "" {
		timeStr = "10:00"
	}

	targetHour := 10
	targetMinute := 0
	parts := strings.Split(timeStr, ":")
	if len(parts) == 2 {
		if h, err := strconv.Atoi(strings.TrimSpace(parts[0])); err == nil && h >= 0 && h < 24 {
			targetHour = h
		}
		if m, err := strconv.Atoi(strings.TrimSpace(parts[1])); err == nil && m >= 0 && m < 60 {
			targetMinute = m
		}
	}

	tzName := os.Getenv("NOTIFICATION_TZ")
	if tzName == "" {
		tzName = "Europe/Moscow"
	}

	loc, err := time.LoadLocation(tzName)
	if err != nil {
		slog.WarnContext(ctx, "failed to load timezone for notifications, falling back to MSK (UTC+3)",
			"tz", tzName,
			"error", err,
		)
		loc = time.FixedZone("MSK", 3*3600)
	}

	slog.InfoContext(ctx, "starting review notifications scheduler",
		"time", fmt.Sprintf("%02d:%02d", targetHour, targetMinute),
		"tz", loc.String(),
	)

	go func() {
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
	}()
}
