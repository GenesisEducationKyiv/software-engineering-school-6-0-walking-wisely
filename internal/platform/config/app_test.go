package config

import (
	"strings"
	"testing"
	"time"
)

func setRequiredAppEnv(t *testing.T) {
	t.Helper()

	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/app")
	t.Setenv("REDIS_URL", "redis://localhost:6379")
	t.Setenv("RESEND_API_KEY", "resend-key")
	t.Setenv("EMAIL_SECRET_KEY", "email-secret")
	t.Setenv("BASE_URL", "https://example.com")
	t.Setenv("FROM_EMAIL", "noreply@example.com")
}

func clearOptionalAppEnv(t *testing.T) {
	t.Helper()

	t.Setenv("REST_PORT", "")
	t.Setenv("GRPC_PORT", "")
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("SERVICE_NAME", "")
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("SCANNER_INTERVAL", "")
	t.Setenv("RESEND_MAX_WAIT", "")
	t.Setenv("EMAIL_CHANNEL_SIZE", "")
}

func TestLoadAppConfig_UsesDefaultsForOptionalEnv(t *testing.T) {
	setRequiredAppEnv(t)
	clearOptionalAppEnv(t)

	cfg, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig returned error: %v", err)
	}

	if cfg.RestPort != "8080" {
		t.Fatalf("RestPort = %q, want 8080", cfg.RestPort)
	}
	if cfg.GrpcPort != "9090" {
		t.Fatalf("GrpcPort = %q, want 9090", cfg.GrpcPort)
	}
	if cfg.GithubToken != "" {
		t.Fatalf("GithubToken = %q, want empty optional token", cfg.GithubToken)
	}
	if cfg.LogLevel != "info" {
		t.Fatalf("LogLevel = %q, want info", cfg.LogLevel)
	}
	if cfg.ServiceName != "github-release-notifier" {
		t.Fatalf("ServiceName = %q, want github-release-notifier", cfg.ServiceName)
	}
	if cfg.Environment != "local" {
		t.Fatalf("Environment = %q, want local", cfg.Environment)
	}
	if cfg.ScannerInterval != 5*time.Minute {
		t.Fatalf("ScannerInterval = %s, want 5m", cfg.ScannerInterval)
	}
	if cfg.ResendMaxWait != 200*time.Millisecond {
		t.Fatalf("ResendMaxWait = %s, want 200ms", cfg.ResendMaxWait)
	}
	if cfg.EmailChannelSize != 1000 {
		t.Fatalf("EmailChannelSize = %d, want 1000", cfg.EmailChannelSize)
	}
}

func TestLoadAppConfig_UsesEnvOverrides(t *testing.T) {
	setRequiredAppEnv(t)

	t.Setenv("REST_PORT", "8081")
	t.Setenv("GRPC_PORT", "9091")
	t.Setenv("GITHUB_TOKEN", "github-token")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("SERVICE_NAME", "release-api")
	t.Setenv("ENVIRONMENT", "prod")
	t.Setenv("SCANNER_INTERVAL", "10m")
	t.Setenv("RESEND_MAX_WAIT", "500ms")
	t.Setenv("EMAIL_CHANNEL_SIZE", "250")

	cfg, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig returned error: %v", err)
	}

	if cfg.RestPort != "8081" {
		t.Fatalf("RestPort = %q, want 8081", cfg.RestPort)
	}
	if cfg.GrpcPort != "9091" {
		t.Fatalf("GrpcPort = %q, want 9091", cfg.GrpcPort)
	}
	if cfg.GithubToken != "github-token" {
		t.Fatalf("GithubToken = %q, want github-token", cfg.GithubToken)
	}
	if cfg.LogLevel != "debug" {
		t.Fatalf("LogLevel = %q, want debug", cfg.LogLevel)
	}
	if cfg.ServiceName != "release-api" {
		t.Fatalf("ServiceName = %q, want release-api", cfg.ServiceName)
	}
	if cfg.Environment != "prod" {
		t.Fatalf("Environment = %q, want prod", cfg.Environment)
	}
	if cfg.ScannerInterval != 10*time.Minute {
		t.Fatalf("ScannerInterval = %s, want 10m", cfg.ScannerInterval)
	}
	if cfg.ResendMaxWait != 500*time.Millisecond {
		t.Fatalf("ResendMaxWait = %s, want 500ms", cfg.ResendMaxWait)
	}
	if cfg.EmailChannelSize != 250 {
		t.Fatalf("EmailChannelSize = %d, want 250", cfg.EmailChannelSize)
	}
}

func TestLoadAppConfig_MissingRequiredEnvReturnsError(t *testing.T) {
	required := []string{
		"DATABASE_URL",
		"REDIS_URL",
		"RESEND_API_KEY",
		"EMAIL_SECRET_KEY",
		"BASE_URL",
		"FROM_EMAIL",
	}

	for _, missing := range required {
		missing := missing
		t.Run(missing, func(t *testing.T) {
			setRequiredAppEnv(t)
			clearOptionalAppEnv(t)
			t.Setenv(missing, "")

			_, err := LoadAppConfig()
			if err == nil || !strings.Contains(err.Error(), "required env var "+missing+" is not set") {
				t.Fatalf("error = %v, want missing %s error", err, missing)
			}
		})
	}
}

func TestLoadAppConfig_InvalidOptionalValuesFallBackToDefaults(t *testing.T) {
	setRequiredAppEnv(t)

	t.Setenv("SCANNER_INTERVAL", "soon")
	t.Setenv("RESEND_MAX_WAIT", "eventually")
	t.Setenv("EMAIL_CHANNEL_SIZE", "many")

	cfg, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig returned error: %v", err)
	}

	if cfg.ScannerInterval != 5*time.Minute {
		t.Fatalf("ScannerInterval = %s, want 5m", cfg.ScannerInterval)
	}
	if cfg.ResendMaxWait != 200*time.Millisecond {
		t.Fatalf("ResendMaxWait = %s, want 200ms", cfg.ResendMaxWait)
	}
	if cfg.EmailChannelSize != 1000 {
		t.Fatalf("EmailChannelSize = %d, want 1000", cfg.EmailChannelSize)
	}
}

func TestLoadAppConfig_NonPositiveOptionalValuesFallBackToDefaults(t *testing.T) {
	setRequiredAppEnv(t)

	t.Setenv("SCANNER_INTERVAL", "-1s")
	t.Setenv("RESEND_MAX_WAIT", "0s")
	t.Setenv("EMAIL_CHANNEL_SIZE", "-1")

	cfg, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig returned error: %v", err)
	}

	if cfg.ScannerInterval != 5*time.Minute {
		t.Fatalf("ScannerInterval = %s, want 5m", cfg.ScannerInterval)
	}
	if cfg.ResendMaxWait != 200*time.Millisecond {
		t.Fatalf("ResendMaxWait = %s, want 200ms", cfg.ResendMaxWait)
	}
	if cfg.EmailChannelSize != 1000 {
		t.Fatalf("EmailChannelSize = %d, want 1000", cfg.EmailChannelSize)
	}
}
