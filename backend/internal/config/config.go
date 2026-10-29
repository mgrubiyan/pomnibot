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
	"github.com/mgrubiyan/pomnibot/backend/internal/providers/openrouter"
	"github.com/mgrubiyan/pomnibot/backend/internal/speechkit"
	maxtransport "github.com/mgrubiyan/pomnibot/backend/internal/transport/max"
	"github.com/mgrubiyan/pomnibot/backend/internal/usecase"
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
	MaxBotToken           string
	TelegramBotToken      string
	AppURL                string
	MaxAPIURL             string
	MaxInsecureSkipVerify bool
	Notifications         maxtransport.NotificationConfig
	GigaChat              gigachat.Config
	// OpenRouter is enabled when OPENROUTER_KEY is set; it goes first unless
	// LLM_PRIMARY explicitly selects a legacy provider.
	OpenRouter *openrouter.Config
	// AIStudio is the Qwen backup, disabled with YC_LLM_MODEL=off.
	AIStudio *aistudio.Config
	LLM      LLMConfig
	OCR      yandex.Config
	// Speech is enabled when YC_API_KEY is set, locally too: voice answers
	// take the OCR's key and folder.
	Speech *speechkit.Config
	// Admins see every complaint: ADMIN_MAX_IDS and ADMIN_TG_IDS, and
	// locally the browser test user.
	Admins usecase.Admins
}

// Models that can go first, see LLMConfig.Primary.
const (
	PrimaryAIStudio   = "aistudio"
	PrimaryGigaChat   = "gigachat"
	PrimaryOpenRouter = "openrouter"
)

// LLMConfig sets which model goes first and when requests move to the other
// one.
type LLMConfig struct {
	// Primary selects the first provider; the remaining providers are backups.
	Primary string
	// PrimaryTimeout bounds one call to the primary, its retries included,
	// so that a provider that hangs leaves time for the backup. Applied to
	// every backend except the final one; a timeout marks it unavailable.
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
	cfg.Admins, err = adminsFromEnv(cfg.Environment == Local)
	if err != nil {
		return Config{}, err
	}
	if speech, ok := speechkit.ConfigFromEnv(); ok {
		cfg.Speech = &speech
	}
	if cfg.Environment == Local {
		return cfg, nil
	}

	cfg.MaxBotToken = value("BOT_TOKEN", "")
	cfg.TelegramBotToken = value("TG_BOT_TOKEN", "")
	domain := value("DOMAIN", "")
	cfg.AppURL = "https://" + domain
	cfg.MaxAPIURL = value("MAX_API_URL", "https://platform-api2.max.ru")
	var errs []error
	for _, field := range []struct{ name, value string }{{"BOT_TOKEN", cfg.MaxBotToken}, {"DOMAIN", domain}} {
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
	if value("OPENROUTER_KEY", "") != "" || value("LLM_PRIMARY", "") == PrimaryOpenRouter {
		router, err := openrouter.ConfigFromEnv()
		cfg.OpenRouter = &router
		errs = append(errs, err)
	}
	cfg.LLM, err = llmFromEnv(cfg.AIStudio != nil, cfg.OpenRouter != nil)
	errs = append(errs, err)
	cfg.Notifications, err = notificationsFromEnv()
	errs = append(errs, err)
	return cfg, errors.Join(errs...)
}

func adminsFromEnv(local bool) (usecase.Admins, error) {
	admins := usecase.Admins{BrowserTestUser: local}
	for _, list := range []struct {
		name string
		dst  *[]int64
	}{{"ADMIN_MAX_IDS", &admins.MaxIDs}, {"ADMIN_TG_IDS", &admins.TgIDs}} {
		for _, field := range strings.FieldsFunc(value(list.name, ""), func(r rune) bool { return r == ',' || r == ' ' }) {
			id, err := strconv.ParseInt(field, 10, 64)
			if err != nil || id <= 0 {
				return usecase.Admins{}, fmt.Errorf("%s: %q is not an ID", list.name, field)
			}
			*list.dst = append(*list.dst, id)
		}
	}
	return admins, nil
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

func llmFromEnv(withAIStudio, withOpenRouter bool) (LLMConfig, error) {
	var cfg LLMConfig
	var errs []error

	switch primary := value("LLM_PRIMARY", ""); {
	case primary == "" && withOpenRouter, primary == PrimaryOpenRouter && withOpenRouter:
		cfg.Primary = PrimaryOpenRouter
	case primary == "" && withAIStudio, primary == PrimaryAIStudio && withAIStudio:
		cfg.Primary = PrimaryAIStudio
	case primary == "", primary == PrimaryGigaChat:
		cfg.Primary = PrimaryGigaChat
	case primary == PrimaryAIStudio:
		errs = append(errs, errors.New("LLM_PRIMARY = aistudio, but YC_LLM_MODEL = off"))
	default:
		errs = append(errs, fmt.Errorf("LLM_PRIMARY = %q, want openrouter, aistudio or gigachat", primary))
	}
	primaryTimeout := "1m"
	if cfg.Primary == PrimaryOpenRouter {
		primaryTimeout = "5m"
	}
	for _, d := range []struct {
		name, fallback string
		dst            *time.Duration
	}{
		{"LLM_PRIMARY_TIMEOUT", primaryTimeout, &cfg.PrimaryTimeout},
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
