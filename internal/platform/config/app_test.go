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
	t.Setenv("NATS_URL", "nats://localhost:4222")
	t.Setenv("EMAIL_SECRET_KEY", "email-secret")
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
	t.Setenv("OUTBOX_CLEANUP_INTERVAL", "")
	t.Setenv("OUTBOX_RETENTION", "")
	t.Setenv("NATS_STREAM_NAME", "")
	t.Setenv("NATS_SUBJECT_PREFIX", "")
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
	if cfg.Outbox.CleanupInterval != 30*time.Minute {
		t.Fatalf("OutboxCleanupInterval = %s, want 30m", cfg.Outbox.CleanupInterval)
	}
	if cfg.Outbox.Retention != 7*24*time.Hour {
		t.Fatalf("OutboxRetention = %s, want 168h", cfg.Outbox.Retention)
	}
	if cfg.NATS.StreamName != "EVENTS" {
		t.Fatalf("NATSStreamName = %q, want EVENTS", cfg.NATS.StreamName)
	}
	if cfg.NATS.SubjectPrefix != "events" {
		t.Fatalf("NATSSubjectPrefix = %q, want events", cfg.NATS.SubjectPrefix)
	}
}

func TestLoadAppConfig_UsesEnvOverrides(t *testing.T) {
	setRequiredAppEnv(t)

	t.Setenv("REST_PORT", "8082")
	t.Setenv("GRPC_PORT", "9091")
	t.Setenv("GITHUB_TOKEN", "github-token")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("SERVICE_NAME", "release-api")
	t.Setenv("ENVIRONMENT", "prod")
	t.Setenv("SCANNER_INTERVAL", "10m")
	t.Setenv("OUTBOX_CLEANUP_INTERVAL", "15m")
	t.Setenv("OUTBOX_RETENTION", "48h")
	t.Setenv("NATS_STREAM_NAME", "DOMAIN")
	t.Setenv("NATS_SUBJECT_PREFIX", "domain_events")

	cfg, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig returned error: %v", err)
	}

	if cfg.RestPort != "8082" {
		t.Fatalf("RestPort = %q, want 8082", cfg.RestPort)
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
	if cfg.Outbox.CleanupInterval != 15*time.Minute {
		t.Fatalf("OutboxCleanupInterval = %s, want 15m", cfg.Outbox.CleanupInterval)
	}
	if cfg.Outbox.Retention != 48*time.Hour {
		t.Fatalf("OutboxRetention = %s, want 48h", cfg.Outbox.Retention)
	}
	if cfg.NATS.StreamName != "DOMAIN" {
		t.Fatalf("NATSStreamName = %q, want DOMAIN", cfg.NATS.StreamName)
	}
	if cfg.NATS.SubjectPrefix != "domain_events" {
		t.Fatalf("NATSSubjectPrefix = %q, want domain_events", cfg.NATS.SubjectPrefix)
	}
}

func TestLoadAppConfig_MissingRequiredEnvReturnsError(t *testing.T) {
	required := []string{
		"DATABASE_URL",
		"REDIS_URL",
		"NATS_URL",
		"EMAIL_SECRET_KEY",
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
	t.Setenv("OUTBOX_CLEANUP_INTERVAL", "often")
	t.Setenv("OUTBOX_RETENTION", "long")

	cfg, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig returned error: %v", err)
	}

	if cfg.ScannerInterval != 5*time.Minute {
		t.Fatalf("ScannerInterval = %s, want 5m", cfg.ScannerInterval)
	}
	if cfg.Outbox.CleanupInterval != 30*time.Minute {
		t.Fatalf("OutboxCleanupInterval = %s, want 30m", cfg.Outbox.CleanupInterval)
	}
	if cfg.Outbox.Retention != 7*24*time.Hour {
		t.Fatalf("OutboxRetention = %s, want 168h", cfg.Outbox.Retention)
	}
}

func TestLoadAppConfig_NonPositiveOptionalValuesFallBackToDefaults(t *testing.T) {
	setRequiredAppEnv(t)

	t.Setenv("SCANNER_INTERVAL", "-1s")
	t.Setenv("OUTBOX_CLEANUP_INTERVAL", "0s")
	t.Setenv("OUTBOX_RETENTION", "-1h")

	cfg, err := LoadAppConfig()
	if err != nil {
		t.Fatalf("LoadAppConfig returned error: %v", err)
	}

	if cfg.ScannerInterval != 5*time.Minute {
		t.Fatalf("ScannerInterval = %s, want 5m", cfg.ScannerInterval)
	}
	if cfg.Outbox.CleanupInterval != 30*time.Minute {
		t.Fatalf("OutboxCleanupInterval = %s, want 30m", cfg.Outbox.CleanupInterval)
	}
	if cfg.Outbox.Retention != 7*24*time.Hour {
		t.Fatalf("OutboxRetention = %s, want 168h", cfg.Outbox.Retention)
	}
}
