package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/ingest"
	"github.com/mgrubiyan/pomnibot/backend/internal/notes"
	maxtransport "github.com/mgrubiyan/pomnibot/backend/internal/transport/max"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

const textLargeFile = "Файл больше 20 МБ. Пришлите файл меньшего размера или разделите конспект на части."

// Bot shares the application user service and Maker with MAX.
type Bot struct {
	client     *Client
	users      usecase.UserService
	maker      *notes.Maker
	appURL     string
	schedule   maxtransport.NotificationConfig
	background sync.WaitGroup
	dialogues  sync.WaitGroup
	jobs       sync.WaitGroup
	mu         sync.Mutex
	lanes      map[int64]chan struct{}
	albums     map[string]*album
}

type album struct {
	messages    []*Message
	userID      int64
	changed     chan struct{}
	first, last time.Time
}

// NewBot constructs the personal-chat Telegram adapter.
func NewBot(client *Client, users usecase.UserService, maker *notes.Maker, appURL string, schedule maxtransport.NotificationConfig) (*Bot, error) {
	if client == nil || users == nil || maker == nil || !ValidAppURL(appURL) {
		return nil, errors.New("telegram bot requires client, users, maker and HTTPS app URL")
	}
	return &Bot{client: client, users: users, maker: maker, appURL: appURL, schedule: schedule, lanes: make(map[int64]chan struct{}), albums: make(map[string]*album)}, nil
}

// Start validates connectivity and starts cancellable polling and reminders.
func (b *Bot) Start(ctx context.Context) error {
	if err := b.client.Start(ctx); err != nil {
		return err
	}
	b.background.Go(func() { b.poll(ctx) })
	if b.schedule.Enabled {
		b.background.Go(func() { b.reminders(ctx) })
	}
	return nil
}

// Wait joins producers before waiting for their dialogue and note jobs.
func (b *Bot) Wait() { b.background.Wait(); b.dialogues.Wait(); b.jobs.Wait() }

func (b *Bot) poll(ctx context.Context) {
	var offset int64
	for ctx.Err() == nil {
		updates, err := b.client.GetUpdates(ctx, offset)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			slog.WarnContext(ctx, "Telegram polling failed", "error", err)
			if pause(ctx, time.Second) != nil {
				return
			}
			continue
		}
		sort.SliceStable(updates, func(i, j int) bool { return updates[i].ID < updates[j].ID })
		for _, update := range updates {
			if update.ID < offset {
				continue
			}
			b.dispatch(ctx, update)
			offset = update.ID + 1
		}
		// Finish short profile operations before confirming this batch.
		// Card generation runs separately and keeps polling responsive.
		b.dialogues.Wait()
	}
}

// A sender's messages form an ordered chain; other senders remain independent.
func (b *Bot) dispatch(ctx context.Context, update Update) {
	message := update.Message
	if message == nil || message.From == nil || message.From.IsBot || message.Chat.Type != "private" {
		return
	}
	userID := message.From.ID
	b.mu.Lock()
	previous := b.lanes[userID]
	done := make(chan struct{})
	b.lanes[userID] = done
	b.mu.Unlock()
	b.dialogues.Go(func() {
		defer func() {
			close(done)
			b.mu.Lock()
			if b.lanes[userID] == done {
				delete(b.lanes, userID)
			}
			b.mu.Unlock()
		}()
		if previous != nil {
			select {
			case <-previous:
			case <-ctx.Done():
				return
			}
		}
		if ctx.Err() != nil {
			return
		}
		if err := b.handleMessage(ctx, message); err != nil {
			slog.ErrorContext(ctx, "Telegram message failed", "error", err)
		}
	})
}

func (b *Bot) send(ctx context.Context, chatID int64, text string, withApp bool) {
	app := ""
	if withApp {
		app = b.appURL
	}
	if _, err := b.client.Send(ctx, chatID, 0, text, app); err != nil && ctx.Err() == nil {
		slog.WarnContext(ctx, "send Telegram message", "error", err)
	}
}

func profilePrompt(account usecase.Account) string {
	if account.ProfileStep == "first_name" {
		return "Теперь введите имя (до 100 символов)."
	}
	return "Введите фамилию (до 100 символов)."
}

func (b *Bot) handleMessage(ctx context.Context, m *Message) error {
	u := m.From
	account, err := b.users.ResolveIdentity(ctx, usecase.Identity{Platform: "telegram", ExternalID: u.ID, FirstName: u.FirstName, LastName: u.LastName, Username: u.Username, IsBot: u.IsBot, BotStarted: true})
	if err != nil {
		b.send(ctx, m.Chat.ID, "Не удалось загрузить профиль. Повторите сообщение чуть позже.", false)
		return err
	}
	text := strings.TrimSpace(m.Text)
	command := ""
	if strings.HasPrefix(text, "/") {
		command = strings.SplitN(strings.Fields(text)[0], "@", 2)[0]
	}
	if command == "/profile" {
		account, err = b.users.ProfileInput(ctx, account.User.ID, "", true)
		if err != nil {
			return err
		}
		b.send(ctx, m.Chat.ID, profilePrompt(account), false)
		return nil
	}
	if command == "/start" {
		if account.ProfileStep != "" || !account.ProfileComplete {
			b.send(ctx, m.Chat.ID, profilePrompt(account), false)
		} else {
			b.welcome(ctx, m.Chat.ID)
		}
		return nil
	}
	if account.ProfileComplete && (m.Document != nil || len(m.Photos) > 0) {
		if m.MediaGroupID != "" {
			b.addAlbum(ctx, account.User.ID, m)
		} else {
			b.jobs.Go(func() { b.process(ctx, account.User.ID, []*Message{m}) })
		}
		return nil
	}
	if account.ProfileStep != "" || !account.ProfileComplete {
		// Captions, commands and attachments never supply a profile answer.
		if command != "" || text == "" || m.Document != nil || len(m.Photos) > 0 {
			b.send(ctx, m.Chat.ID, profilePrompt(account), false)
			return nil
		}
		account, err = b.users.ProfileInput(ctx, account.User.ID, text, false)
		if errors.Is(err, usecase.ErrValidation) {
			b.send(ctx, m.Chat.ID, "Введите непустой текст длиной до 100 символов.\n"+profilePrompt(account), false)
			return nil
		}
		if err != nil {
			return err
		}
		if account.ProfileStep == "" {
			b.welcome(ctx, m.Chat.ID)
		} else {
			b.send(ctx, m.Chat.ID, profilePrompt(account), false)
		}
		return nil
	}

	b.welcome(ctx, m.Chat.ID)
	return nil
}

func (b *Bot) welcome(ctx context.Context, chatID int64) {
	b.send(ctx, chatID, "Профиль сохранён. Пришлите конспект файлом, PDF, Word, презентацией или фотографиями — я создам карточки для повторения. Изменить имя: /profile", true)
}

func (b *Bot) addAlbum(ctx context.Context, userID int64, m *Message) {
	key := fmt.Sprintf("%d:%s", m.Chat.ID, m.MediaGroupID)
	b.mu.Lock()
	a := b.albums[key]
	if a == nil {
		a = &album{userID: userID, changed: make(chan struct{}, 1), first: time.Now()}
		b.albums[key] = a
		b.jobs.Go(func() { b.collectAlbum(ctx, key, a) })
	}
	a.messages = append(a.messages, m)
	a.last = time.Now()
	select {
	case a.changed <- struct{}{}:
	default:
	}
	b.mu.Unlock()
}

func (b *Bot) collectAlbum(ctx context.Context, key string, a *album) {
	for {
		b.mu.Lock()
		deadline := minTime(a.last.Add(time.Second), a.first.Add(3*time.Second))
		b.mu.Unlock()
		timer := time.NewTimer(time.Until(deadline))
		select {
		case <-ctx.Done():
			timer.Stop()
			b.mu.Lock()
			delete(b.albums, key)
			b.mu.Unlock()
			return
		case <-a.changed:
			timer.Stop()
			continue
		case <-timer.C:
		}
		b.mu.Lock()
		// A message may have arrived while the timer fired.
		if time.Now().Before(minTime(a.last.Add(time.Second), a.first.Add(3*time.Second))) {
			b.mu.Unlock()
			continue
		}
		messages := append([]*Message(nil), a.messages...)
		delete(b.albums, key)
		b.mu.Unlock()
		sort.SliceStable(messages, func(i, j int) bool { return messages[i].ID < messages[j].ID })
		b.process(ctx, a.userID, messages)
		return
	}
}
func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}
	return b
}

func messageFile(m *Message) File {
	if m.Document != nil {
		return *m.Document
	}
	var best File
	for _, photo := range m.Photos {
		if photo.Width*photo.Height > best.Width*best.Height || best.ID == "" {
			best = photo
		}
	}
	if best.Name == "" {
		best.Name = "photo.jpg"
	}
	return best
}

func (b *Bot) process(ctx context.Context, userID int64, messages []*Message) {
	chatID := messages[0].Chat.ID
	for _, m := range messages {
		if messageFile(m).Size > MaxFileSize {
			b.send(ctx, chatID, textLargeFile, false)
			return
		}
	}
	mid, _ := b.client.Send(ctx, chatID, 0, notes.ReceivedText(len(messages)), "")
	var mu sync.Mutex
	var last time.Time
	status := func(text string) {
		mu.Lock()
		defer mu.Unlock()
		if _, err := b.client.Send(ctx, chatID, mid, text, ""); err != nil {
			b.send(ctx, chatID, text, false)
		}
	}
	files := make([]ingest.File, 0, len(messages))
	caption := ""
	for _, message := range messages {
		file := messageFile(message)
		data, err := b.client.Download(ctx, file.ID)
		if err != nil {
			if errors.Is(err, errFileTooLarge) {
				status(textLargeFile)
			} else {
				status(notes.TextResend)
			}
			return
		}
		name := file.Name
		if name == "" {
			name = "document"
		}
		files = append(files, ingest.File{Name: name, Data: data})
		if caption == "" {
			caption = message.Caption
		}
	}
	result, err := b.maker.Make(ctx, notes.Request{UserID: userID, Caption: caption, Files: files}, notes.Progress{
		Queued: func(ahead int) {
			status(notes.QueuedText(len(messages), ahead))
		},
		Reading:    func() { status(notes.TextReading) },
		Generating: func() { status(notes.TextGenerating) },
		Cards: func(total int) {
			mu.Lock()
			due := time.Since(last) >= 1500*time.Millisecond
			if due {
				last = time.Now()
			}
			mu.Unlock()
			if due {
				status(notes.CardsText(total))
			}
		},
	})
	if err != nil {
		if text := notes.FailureText(err); text != "" {
			status(text)
		}
		return
	}
	text := notes.SuccessText(result)
	if _, err = b.client.Send(ctx, chatID, mid, text, b.appURL); err != nil {
		b.send(ctx, chatID, text, true)
	}
}

// SendReviewReminders delivers only to Telegram users who started a private chat
// and completed their profile. Learning services always receive internal IDs.
func (b *Bot) SendReviewReminders(ctx context.Context) (int, error) {
	loc := b.schedule.Location
	if loc == nil {
		loc = time.UTC
	}
	ids, err := b.users.GetUsersWithDueFacts(ctx, time.Now(), loc.String())
	if err != nil {
		return 0, err
	}
	count := 0
	for _, id := range ids {
		identities, err := b.users.GetDeliveryIdentities(ctx, id)
		if err != nil {
			return count, err
		}
		for _, identity := range identities {
			if identity.Platform != "telegram" {
				continue
			}
			if _, err = b.client.Send(ctx, identity.ExternalID, 0, "⏰ Пора повторить материал — в приложении вас ждут карточки на сегодня. 📚", b.appURL); err == nil {
				count++
			}
			if err = pause(ctx, 50*time.Millisecond); err != nil {
				return count, err
			}
		}
	}
	return count, nil
}

func (b *Bot) reminders(ctx context.Context) {
	loc := b.schedule.Location
	if loc == nil {
		loc = time.UTC
	}
	for ctx.Err() == nil {
		now := time.Now().In(loc)
		next := time.Date(now.Year(), now.Month(), now.Day(), b.schedule.Hour, b.schedule.Minute, 0, 0, loc)
		if !next.After(now) {
			next = next.AddDate(0, 0, 1)
		}
		if pause(ctx, time.Until(next)) != nil {
			return
		}
		if _, err := b.SendReviewReminders(ctx); err != nil && ctx.Err() == nil {
			slog.ErrorContext(ctx, "Telegram reminders failed", "error", err)
		}
	}
}
