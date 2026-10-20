// Package config loads and validates the server environment at startup.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/mgrubiyan/pomnibot/backend/internal/ingest/yandex"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/aistudio"
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/gigachat"
	maxtransport "github.com/mgrubiyan/pomnibot/backend/internal/transport/max"
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
	Notifications         maxtransport.NotificationConfig
	GigaChat              gigachat.Config
	// AIStudio is the main model, Qwen by default: it writes the cards,
	// grades answers and reads photos, GigaChat backs it up (see LLM). nil
	// with YC_LLM_MODEL=off.
	AIStudio *aistudio.Config
	LLM      LLMConfig
	OCR      yandex.Config
}

// Models that can go first, see LLMConfig.Primary.
const (
	PrimaryAIStudio = "aistudio"
	PrimaryGigaChat = "gigachat"
)

// LLMConfig sets which model goes first and when requests move to the other
// one.
type LLMConfig struct {
	// Primary is PrimaryAIStudio or PrimaryGigaChat; GigaChat without AI
	// Studio.
	Primary string
	// PrimaryTimeout bounds one call to the primary, its retries included,
	// so that a primary that hangs leaves time for the backup. It counts as
	// down then.
	PrimaryTimeout time.Duration
	// Cooldown is how long a provider that is down gets no requests.
	Cooldown time.Duration
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
	if value("YC_LLM_MODEL", "") != "off" {
		studio, err := aistudio.ConfigFromEnv()
		cfg.AIStudio = &studio
		errs = append(errs, err)
	}
	cfg.LLM, err = llmFromEnv(cfg.AIStudio != nil)
	errs = append(errs, err)
	cfg.Notifications, err = notificationsFromEnv()
	errs = append(errs, err)
	return cfg, errors.Join(errs...)
}

func notificationsFromEnv() (maxtransport.NotificationConfig, error) {
	enabled, err := strconv.ParseBool(value("NOTIFICATIONS_ENABLED", "true"))
	if err != nil {
		return maxtransport.NotificationConfig{}, fmt.Errorf("NOTIFICATIONS_ENABLED: %w", err)
	}
	at, err := time.Parse("15:04", value("NOTIFICATION_TIME", "10:00"))
	if err != nil {
		return maxtransport.NotificationConfig{}, fmt.Errorf("NOTIFICATION_TIME: %w", err)
	}
	loc, err := time.LoadLocation(value("NOTIFICATION_TZ", "Europe/Moscow"))
	if err != nil {
		return maxtransport.NotificationConfig{}, fmt.Errorf("NOTIFICATION_TZ: %w", err)
	}
	return maxtransport.NotificationConfig{Enabled: enabled, Hour: at.Hour(), Minute: at.Minute(), Location: loc}, nil
}

func llmFromEnv(withAIStudio bool) (LLMConfig, error) {
	var cfg LLMConfig
	var errs []error

	switch primary := value("LLM_PRIMARY", ""); {
	case primary == "" && withAIStudio, primary == PrimaryAIStudio && withAIStudio:
		cfg.Primary = PrimaryAIStudio
	case primary == "", primary == PrimaryGigaChat:
		cfg.Primary = PrimaryGigaChat
	case primary == PrimaryAIStudio:
		errs = append(errs, errors.New("LLM_PRIMARY = aistudio, but YC_LLM_MODEL = off"))
	default:
		errs = append(errs, fmt.Errorf("LLM_PRIMARY = %q, want aistudio or gigachat", primary))
	}
	for _, d := range []struct {
		name, fallback string
		dst            *time.Duration
	}{
		{"LLM_PRIMARY_TIMEOUT", "1m", &cfg.PrimaryTimeout},
		{"LLM_COOLDOWN", "10m", &cfg.Cooldown},
	} {
		v, err := time.ParseDuration(value(d.name, d.fallback))
		if err != nil || v <= 0 {
			errs = append(errs, fmt.Errorf("%s = %q, want a positive duration such as %s", d.name, value(d.name, d.fallback), d.fallback))
			continue
		}
		*d.dst = v
	}
	return cfg, errors.Join(errs...)
}
