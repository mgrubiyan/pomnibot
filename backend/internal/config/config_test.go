package config

import (
	"strings"
	"testing"
)

func TestLoadLocal(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	for _, key := range []string{"BOT_TOKEN", "GIGACHAT_AUTH_KEY", "GIGACHAT_CLIENT_ID", "GIGACHAT_CLIENT_SECRET", "YC_API_KEY", "YC_FOLDER_ID", "PORT", "DATABASE_URL"} {
		t.Setenv(key, "")
	}
	t.Setenv("BOT_TOKEN", "ignored-in-local")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BotToken != "" {
		t.Fatal("local mode must ignore bot credentials")
	}
}

func TestLoadRejectsUnknownEnvironment(t *testing.T) {
	t.Setenv("APP_ENV", "staging")
	if _, err := Load(); err == nil {
		t.Fatal("expected unknown environment to fail")
	}
}

func TestLoadDefaultsToProd(t *testing.T) {
	t.Setenv("APP_ENV", "")
	t.Setenv("BOT_TOKEN", "")
	t.Setenv("DATABASE_URL", "postgres://localhost/pomnibot")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "BOT_TOKEN") {
		t.Fatal("missing environment must not enable local mode")
	}
}

func TestLoadValidatesPort(t *testing.T) {
	t.Setenv("APP_ENV", "local")
	t.Setenv("PORT", "70000")
	if _, err := Load(); err == nil {
		t.Fatal("expected invalid port to fail")
	}
}

func TestLoadProd(t *testing.T) {
	values := map[string]string{
		"APP_ENV": "prod", "PORT": "8081", "DATABASE_URL": "postgres://localhost/pomnibot",
		"BOT_TOKEN": "test-token", "DOMAIN": "example.test", "MAX_API_URL": "https://max.example.test",
		"MAX_INSECURE_SKIP_VERIFY": "false", "GIGACHAT_AUTH_KEY": "test-key",
		"GIGACHAT_SCOPE": "GIGACHAT_API_PERS", "GIGACHAT_MODEL": "test-model",
		"GIGACHAT_FALLBACK_MODEL": "fallback", "GIGACHAT_BASE_URL": "https://example.test/v1",
		"GIGACHAT_AUTH_URL": "https://example.test/oauth", "GIGACHAT_MAX_TOKENS": "8192",
		"YC_API_KEY": "test-key", "YC_FOLDER_ID": "test-folder",
		"YC_OCR_BASE_URL": "https://example.test", "YC_OCR_OPERATION_URL": "https://example.test",
		"NOTIFICATIONS_ENABLED": "false", "NOTIFICATION_TIME": "09:30", "NOTIFICATION_TZ": "UTC",
	}
	for key, value := range values {
		t.Setenv(key, value)
	}
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"BOT_TOKEN", "GIGACHAT_AUTH_KEY", "GIGACHAT_CLIENT_ID", "GIGACHAT_CLIENT_SECRET", "YC_API_KEY"} {
		t.Setenv(key, "")
	}
	_, err := Load()
	if err == nil {
		t.Fatal("production must require integration credentials")
	}
	for _, key := range []string{"BOT_TOKEN", "GIGACHAT_AUTH_KEY", "YC_API_KEY"} {
		if !strings.Contains(err.Error(), key) {
			t.Fatalf("missing validation error for %s", key)
		}
	}

}
