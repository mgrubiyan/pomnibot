package app

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/config"
	"github.com/mgrubiyan/pomnibot/backend/internal/notes"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
)

type telegramTransport func(*http.Request) (*http.Response, error)

func (f telegramTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func telegramResponse(status int, body string) (*http.Response, error) {
	return &http.Response{StatusCode: status, Body: io.NopCloser(strings.NewReader(body)), Header: make(http.Header)}, nil
}

func captureTelegramLogs(t *testing.T) *bytes.Buffer {
	t.Helper()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(previous) })
	return &logs
}

func mockTelegram(t *testing.T, transport telegramTransport) {
	t.Helper()
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	t.Cleanup(func() { http.DefaultTransport = previous })
}

// Only construction is exercised; Telegram must not call user services here.
type telegramUsers struct{ usecase.UserService }

func TestTelegramWithoutToken(t *testing.T) {
	logs := captureTelegramLogs(t)
	mockTelegram(t, func(*http.Request) (*http.Response, error) {
		t.Error("Telegram must not make requests without a token")
		return nil, errors.New("unexpected request")
	})
	wait := startTelegramBot(context.Background(), config.Config{}, nil, nil)
	wait()
	if !strings.Contains(logs.String(), "TG_BOT_TOKEN is not set") {
		t.Fatalf("missing token must be logged: %s", logs.String())
	}
}

func TestTelegramStartupFailure(t *testing.T) {
	for _, failure := range []string{"initialization", "getMe", "deleteWebhook", "network"} {
		t.Run(failure, func(t *testing.T) {
			logs := captureTelegramLogs(t)
			mockTelegram(t, func(r *http.Request) (*http.Response, error) {
				if failure == "network" {
					return nil, errors.New("network unavailable")
				}
				if strings.HasSuffix(r.URL.Path, failure) {
					return telegramResponse(http.StatusUnauthorized, `{"ok":false,"error_code":401}`)
				}
				return telegramResponse(http.StatusOK, `{"ok":true,"result":true}`)
			})
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			cfg := config.Config{TelegramBotToken: "SECRET", AppURL: "https://example.test"}
			if failure == "initialization" {
				cfg.AppURL = "invalid"
			}
			wait := startTelegramBot(ctx, cfg, &notes.Maker{}, &telegramUsers{})
			wait()
			if ctx.Err() != nil {
				t.Fatal("Telegram failure must not cancel the application")
			}
			if !strings.Contains(logs.String(), "Telegram bot failed") || strings.Contains(logs.String(), "SECRET") {
				t.Fatalf("expected failure log without credentials: %s", logs.String())
			}
		})
	}
}

func TestTelegramSlowStartupDoesNotBlock(t *testing.T) {
	logs := captureTelegramLogs(t)
	entered := make(chan struct{})
	mockTelegram(t, func(r *http.Request) (*http.Response, error) {
		close(entered)
		<-r.Context().Done()
		return nil, r.Context().Err()
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	returned := make(chan func(), 1)
	go func() {
		returned <- startTelegramBot(ctx, config.Config{TelegramBotToken: "SECRET", AppURL: "https://example.test"}, &notes.Maker{}, &telegramUsers{})
	}()
	select {
	case wait := <-returned:
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("Telegram startup did not begin")
		}
		cancel()
		wait()
	case <-time.After(time.Second):
		t.Fatal("Telegram startup blocked the application")
	}
	if strings.Contains(logs.String(), "Telegram bot failed") {
		t.Fatalf("normal shutdown must not be logged as failure: %s", logs.String())
	}
}

func TestTelegramPollingFailure(t *testing.T) {
	logs := captureTelegramLogs(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	polls := 0
	mockTelegram(t, func(r *http.Request) (*http.Response, error) {
		if strings.HasSuffix(r.URL.Path, "getUpdates") {
			polls++
			if polls == 2 {
				cancel()
				return nil, ctx.Err()
			}
			return telegramResponse(http.StatusServiceUnavailable, `{"ok":false,"error_code":503}`)
		}
		return telegramResponse(http.StatusOK, `{"ok":true,"result":true}`)
	})
	wait := startTelegramBot(ctx, config.Config{TelegramBotToken: "SECRET", AppURL: "https://example.test"}, &notes.Maker{}, &telegramUsers{})
	wait()
	if polls != 2 || !strings.Contains(logs.String(), "Telegram polling failed") {
		t.Fatalf("expected polling error log and retry; polls=%d, logs=%s", polls, logs.String())
	}
}
