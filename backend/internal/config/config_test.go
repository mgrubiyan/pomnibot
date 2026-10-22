package config

import (
	"strings"
	"testing"
	"time"
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
	if cfg.MaxBotToken != "" {
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

func setProdEnv(t *testing.T) {
	t.Helper()
	values := map[string]string{
		"APP_ENV": "prod", "PORT": "8081", "DATABASE_URL": "postgres://localhost/pomnibot",
		"TG_BOT_TOKEN": "telegram-test-token", "BOT_TOKEN": "test-token", "DOMAIN": "example.test", "MAX_API_URL": "https://max.example.test",
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
	for _, key := range []string{"OPENROUTER_KEY", "OPENROUTER_MODEL", "OPENROUTER_BASE_URL", "OPENROUTER_MAX_TOKENS", "OPENROUTER_EFFORT", "YC_LLM_MODEL", "YC_LLM_BASE_URL", "YC_LLM_MAX_TOKENS", "LLM_PRIMARY", "LLM_PRIMARY_TIMEOUT", "LLM_COOLDOWN"} {
		t.Setenv(key, "")
	}
}

func TestLoadOpenRouter(t *testing.T) {
	setProdEnv(t)
	t.Setenv("OPENROUTER_KEY", "router-key")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.OpenRouter == nil || cfg.OpenRouter.APIKey != "router-key" || cfg.OpenRouter.Model != "anthropic/claude-haiku-5.5" || cfg.LLM.Primary != PrimaryOpenRouter {
		t.Fatalf("OpenRouter not selected: primary=%s", cfg.LLM.Primary)
	}
	if cfg.LLM.PrimaryTimeout != 5*time.Minute {
		t.Fatal("thinking primary would be interrupted before the measured 166-second response")
	}
	t.Setenv("LLM_PRIMARY", "gigachat")
	cfg, err = Load()
	if err != nil || cfg.LLM.Primary != PrimaryGigaChat || cfg.OpenRouter == nil {
		t.Fatalf("explicit primary ignored: %v", err)
	}
	t.Setenv("YC_LLM_MODEL", "off")
	t.Setenv("LLM_PRIMARY", "openrouter")
	if _, err := Load(); err != nil {
		t.Fatal(err)
	}
}

func TestLoadOpenRouterRequiresKey(t *testing.T) {
	setProdEnv(t)
	t.Setenv("LLM_PRIMARY", "openrouter")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "OPENROUTER_KEY") {
		t.Fatalf("err=%v", err)
	}
}

func TestLoadProd(t *testing.T) {
	setProdEnv(t)
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

func TestLoadProdWithoutTelegramToken(t *testing.T) {
	for _, token := range []string{"", "   "} {
		t.Run("token="+token, func(t *testing.T) {
			setProdEnv(t)
			t.Setenv("TG_BOT_TOKEN", token)
			cfg, err := Load()
			if err != nil {
				t.Fatalf("production must work without Telegram: %v", err)
			}
			if cfg.TelegramBotToken != "" || cfg.MaxBotToken != "test-token" {
				t.Fatal("expected Telegram to be disabled and MAX to remain configured")
			}
		})
	}
}

// Qwen on Yandex AI Studio does it all by default: it serves two requests at
// once against the GigaChat plan's one, and its key and folder are the OCR's.
func TestLoadProdDefaultsToQwenFirst(t *testing.T) {
	setProdEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIStudio == nil || cfg.AIStudio.Model != "qwen3.6-35b-a3b" || cfg.AIStudio.APIKey != "test-key" ||
		cfg.AIStudio.FolderID != "test-folder" {
		t.Errorf("AIStudio = %+v, want Qwen with the shared Yandex Cloud key and folder", cfg.AIStudio)
	}
	if cfg.LLM.Primary != PrimaryAIStudio || cfg.LLM.PrimaryTimeout != time.Minute || cfg.LLM.Cooldown != 10*time.Minute {
		t.Errorf("LLM = %+v, want AI Studio first and the default timings", cfg.LLM)
	}
}

func TestLoadProdWithoutAIStudio(t *testing.T) {
	setProdEnv(t)
	t.Setenv("YC_LLM_MODEL", "off")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIStudio != nil || cfg.LLM.Primary != PrimaryGigaChat {
		t.Errorf("AIStudio = %+v, primary %q; want GigaChat alone", cfg.AIStudio, cfg.LLM.Primary)
	}
}

func TestLoadProdLLMSettings(t *testing.T) {
	setProdEnv(t)
	t.Setenv("YC_LLM_MODEL", "gemma-3-27b-it")
	t.Setenv("LLM_PRIMARY", "gigachat")
	t.Setenv("LLM_PRIMARY_TIMEOUT", "45s")
	t.Setenv("LLM_COOLDOWN", "5m")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.AIStudio == nil || cfg.AIStudio.Model != "gemma-3-27b-it" {
		t.Errorf("AIStudio = %+v", cfg.AIStudio)
	}
	if cfg.LLM.Primary != PrimaryGigaChat || cfg.LLM.PrimaryTimeout != 45*time.Second || cfg.LLM.Cooldown != 5*time.Minute {
		t.Errorf("LLM = %+v", cfg.LLM)
	}
}

func TestLoadProdValidatesLLMSettings(t *testing.T) {
	for _, tc := range []struct{ key, val string }{
		{"LLM_PRIMARY_TIMEOUT", "soon"},
		{"LLM_PRIMARY_TIMEOUT", "0s"},
		{"LLM_COOLDOWN", "-1m"},
		{"YC_LLM_MAX_TOKENS", "many"},
		{"LLM_PRIMARY", "yandexgpt"},
	} {
		t.Run(tc.key+"="+tc.val, func(t *testing.T) {
			setProdEnv(t)
			t.Setenv(tc.key, tc.val)
			if _, err := Load(); err == nil || !strings.Contains(err.Error(), tc.key) {
				t.Errorf("Load() error = %v, want one naming %s", err, tc.key)
			}
		})
	}
}

func TestLoadProdRejectsAIStudioFirstWhenOff(t *testing.T) {
	setProdEnv(t)
	t.Setenv("YC_LLM_MODEL", "off")
	t.Setenv("LLM_PRIMARY", "aistudio")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "LLM_PRIMARY") {
		t.Errorf("Load() error = %v, want one naming LLM_PRIMARY", err)
	}
}
