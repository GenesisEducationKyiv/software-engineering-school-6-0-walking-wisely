package config

import (
	"strings"
	"testing"
	"time"
)

func setRequiredNotificationsEnv(t *testing.T) {
	t.Helper()

	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/app")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("RESEND_API_KEY", "resend-key")
	t.Setenv("FROM_EMAIL", "noreply@example.com")
	t.Setenv("BASE_URL", "https://example.com")
}

func clearOptionalNotificationsEnv(t *testing.T) {
	t.Helper()

	t.Setenv("HTTP_PORT", "")
	t.Setenv("STREAM_KEY", "")
	t.Setenv("STREAM_GROUP", "")
	t.Setenv("STREAM_BATCH_SIZE", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("SERVICE_NAME", "")
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("RESEND_MAX_WAIT", "")
}

func TestLoadNotificationsConfig_UsesDefaultsForOptionalEnv(t *testing.T) {
	setRequiredNotificationsEnv(t)
	clearOptionalNotificationsEnv(t)

	cfg, err := LoadNotificationsConfig()
	if err != nil {
		t.Fatalf("LoadNotificationsConfig returned error: %v", err)
	}

	if cfg.HTTPPort != "8081" {
		t.Fatalf("HTTPPort = %q, want 8081", cfg.HTTPPort)
	}
	if cfg.StreamKey != "events" {
		t.Fatalf("StreamKey = %q, want events", cfg.StreamKey)
	}
	if cfg.StreamGroup != "notifications" {
		t.Fatalf("StreamGroup = %q, want notifications", cfg.StreamGroup)
	}
	if cfg.StreamBatchSize != 32 {
		t.Fatalf("StreamBatchSize = %d, want 32", cfg.StreamBatchSize)
	}
	if cfg.ResendMaxWait != 200*time.Millisecond {
		t.Fatalf("ResendMaxWait = %s, want 200ms", cfg.ResendMaxWait)
	}
}

func TestLoadNotificationsConfig_UsesEnvOverrides(t *testing.T) {
	setRequiredNotificationsEnv(t)

	t.Setenv("HTTP_PORT", "8082")
	t.Setenv("STREAM_KEY", "domain-events")
	t.Setenv("STREAM_GROUP", "email-workers")
	t.Setenv("STREAM_BATCH_SIZE", "64")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("SERVICE_NAME", "emails")
	t.Setenv("ENVIRONMENT", "prod")
	t.Setenv("RESEND_MAX_WAIT", "500ms")

	cfg, err := LoadNotificationsConfig()
	if err != nil {
		t.Fatalf("LoadNotificationsConfig returned error: %v", err)
	}

	if cfg.HTTPPort != "8082" {
		t.Fatalf("HTTPPort = %q, want 8082", cfg.HTTPPort)
	}
	if cfg.StreamKey != "domain-events" {
		t.Fatalf("StreamKey = %q, want domain-events", cfg.StreamKey)
	}
	if cfg.StreamGroup != "email-workers" {
		t.Fatalf("StreamGroup = %q, want email-workers", cfg.StreamGroup)
	}
	if cfg.StreamBatchSize != 64 {
		t.Fatalf("StreamBatchSize = %d, want 64", cfg.StreamBatchSize)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("LogLevel = %q, want debug", cfg.LogLevel)
	}
	if cfg.ServiceName != "emails" {
		t.Fatalf("ServiceName = %q, want emails", cfg.ServiceName)
	}
	if cfg.Environment != "prod" {
		t.Fatalf("Environment = %q, want prod", cfg.Environment)
	}
	if cfg.ResendMaxWait != 500*time.Millisecond {
		t.Fatalf("ResendMaxWait = %s, want 500ms", cfg.ResendMaxWait)
	}
}

func TestLoadNotificationsConfig_MissingRequiredEnvReturnsError(t *testing.T) {
	required := []string{
		"DATABASE_URL",
		"REDIS_URL",
		"RESEND_API_KEY",
		"FROM_EMAIL",
		"BASE_URL",
	}

	for _, missing := range required {
		missing := missing
		t.Run(missing, func(t *testing.T) {
			setRequiredNotificationsEnv(t)
			clearOptionalNotificationsEnv(t)
			t.Setenv(missing, "")

			_, err := LoadNotificationsConfig()
			if err == nil || !strings.Contains(err.Error(), "required env var "+missing+" is not set") {
				t.Fatalf("error = %v, want missing %s error", err, missing)
			}
		})
	}
}

func TestLoadNotificationsConfig_InvalidOptionalValuesFallBackToDefaults(t *testing.T) {
	setRequiredNotificationsEnv(t)

	t.Setenv("STREAM_BATCH_SIZE", "many")
	t.Setenv("RESEND_MAX_WAIT", "soon")

	cfg, err := LoadNotificationsConfig()
	if err != nil {
		t.Fatalf("LoadNotificationsConfig returned error: %v", err)
	}

	if cfg.StreamBatchSize != 32 {
		t.Fatalf("StreamBatchSize = %d, want 32", cfg.StreamBatchSize)
	}
	if cfg.ResendMaxWait != 200*time.Millisecond {
		t.Fatalf("ResendMaxWait = %s, want 200ms", cfg.ResendMaxWait)
	}
}

func TestLoadNotificationsConfig_NonPositiveOptionalValuesFallBackToDefaults(t *testing.T) {
	setRequiredNotificationsEnv(t)

	t.Setenv("STREAM_BATCH_SIZE", "0")
	t.Setenv("RESEND_MAX_WAIT", "-1s")

	cfg, err := LoadNotificationsConfig()
	if err != nil {
		t.Fatalf("LoadNotificationsConfig returned error: %v", err)
	}

	if cfg.StreamBatchSize != 32 {
		t.Fatalf("StreamBatchSize = %d, want 32", cfg.StreamBatchSize)
	}
	if cfg.ResendMaxWait != 200*time.Millisecond {
		t.Fatalf("ResendMaxWait = %s, want 200ms", cfg.ResendMaxWait)
	}
}
