// Package config loads and validates the server environment at startup.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/bot"
	"github.com/mgrubiyan/pomnibot/backend/internal/ingest/yandex"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/gigachat"
)

// Environment selects the local or production runtime.
type Environment string

// Supported runtime environments.
const (
	Local Environment = "local"
	Prod  Environment = "prod"
)

// Config contains validated server settings.
type Config struct {
	Environment           Environment
	Port                  string
	DatabaseURL           string
	BotToken              string
	AppURL                string
	MaxAPIURL             string
	MaxInsecureSkipVerify bool
	Notifications         bot.NotificationConfig
	GigaChat              gigachat.Config
	OCR                   yandex.Config
}

func value(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

// Load defaults to production so an unset APP_ENV never disables integrations.
// Task loads the root .env; deployed containers receive environment from Compose.
func Load() (Config, error) {
	cfg := Config{
		Environment: Environment(value("APP_ENV", string(Prod))),
		Port:        value("PORT", "8080"),
		DatabaseURL: value("DATABASE_URL", ""),
	}
	switch cfg.Environment {
	case Local:
		cfg.DatabaseURL = value("DATABASE_URL", "postgres://pomnibot:pomnibot@localhost:5432/pomnibot?sslmode=disable")
	case Prod:
	default:
		return Config{}, fmt.Errorf("APP_ENV must be local or prod")
	}
	port, err := strconv.Atoi(cfg.Port)
	if err != nil || port < 1 || port > 65535 {
		return Config{}, errors.New("PORT must be between 1 and 65535")
	}
	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if cfg.Environment == Local {
		return cfg, nil
	}

	cfg.BotToken = value("BOT_TOKEN", "")
	domain := value("DOMAIN", "")
	cfg.AppURL = "https://" + domain
	cfg.MaxAPIURL = value("MAX_API_URL", "https://platform-api2.max.ru")
	var errs []error
	for _, field := range []struct{ name, value string }{{"BOT_TOKEN", cfg.BotToken}, {"DOMAIN", domain}} {
		if field.value == "" {
			errs = append(errs, fmt.Errorf("%s is required", field.name))
		}
	}
	cfg.MaxInsecureSkipVerify, err = strconv.ParseBool(value("MAX_INSECURE_SKIP_VERIFY", "false"))
	if err != nil {
		errs = append(errs, fmt.Errorf("MAX_INSECURE_SKIP_VERIFY: %w", err))
	}
	cfg.GigaChat, err = gigachat.ConfigFromEnv()
	errs = append(errs, err)
	cfg.OCR, err = yandex.ConfigFromEnv()
	errs = append(errs, err)
	cfg.Notifications, err = notificationsFromEnv()
	errs = append(errs, err)
	return cfg, errors.Join(errs...)
}

func notificationsFromEnv() (bot.NotificationConfig, error) {
	enabled, err := strconv.ParseBool(value("NOTIFICATIONS_ENABLED", "true"))
	if err != nil {
		return bot.NotificationConfig{}, fmt.Errorf("NOTIFICATIONS_ENABLED: %w", err)
	}
	at, err := time.Parse("15:04", value("NOTIFICATION_TIME", "10:00"))
	if err != nil {
		return bot.NotificationConfig{}, fmt.Errorf("NOTIFICATION_TIME: %w", err)
	}
	loc, err := time.LoadLocation(value("NOTIFICATION_TZ", "Europe/Moscow"))
	if err != nil {
		return bot.NotificationConfig{}, fmt.Errorf("NOTIFICATION_TZ: %w", err)
	}
	return bot.NotificationConfig{Enabled: enabled, Hour: at.Hour(), Minute: at.Minute(), Location: loc}, nil
}
