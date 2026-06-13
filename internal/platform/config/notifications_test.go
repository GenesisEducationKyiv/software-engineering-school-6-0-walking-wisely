package config

import (
	"strings"
	"testing"
	"time"
)

func setRequiredNotificationsEnv(t *testing.T) {
	t.Helper()

	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/app")
	t.Setenv("NATS_URL", "nats://localhost:4222")
	t.Setenv("RESEND_API_KEY", "resend-key")
	t.Setenv("FROM_EMAIL", "noreply@example.com")
	t.Setenv("BASE_URL", "https://example.com")
}

func clearOptionalNotificationsEnv(t *testing.T) {
	t.Helper()

	t.Setenv("HTTP_PORT", "")
	t.Setenv("NATS_STREAM_NAME", "")
	t.Setenv("NATS_SUBJECT_PREFIX", "")
	t.Setenv("NATS_CONSUMER_NAME", "")
	t.Setenv("NATS_BATCH_SIZE", "")
	t.Setenv("NATS_ACK_WAIT", "")
	t.Setenv("NATS_MAX_DELIVERIES", "")
	t.Setenv("NATS_DLQ_SUBJECT", "")
	t.Setenv("LOG_LEVEL", "")
	t.Setenv("SERVICE_NAME", "")
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("RESEND_MAX_WAIT", "")
	t.Setenv("NOTIFICATION_JOB_INSERT_BATCH_SIZE", "")
	t.Setenv("NOTIFICATION_JOB_CLEANUP_INTERVAL", "")
	t.Setenv("NOTIFICATION_JOB_RETENTION", "")
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
	if cfg.NATSStreamName != "EVENTS" {
		t.Fatalf("NATSStreamName = %q, want EVENTS", cfg.NATSStreamName)
	}
	if cfg.NATSSubjectPrefix != "events" {
		t.Fatalf("NATSSubjectPrefix = %q, want events", cfg.NATSSubjectPrefix)
	}
	if cfg.NATSConsumerName != "notifications" {
		t.Fatalf("NATSConsumerName = %q, want notifications", cfg.NATSConsumerName)
	}
	if cfg.NATSBatchSize != 32 {
		t.Fatalf("NATSBatchSize = %d, want 32", cfg.NATSBatchSize)
	}
	if cfg.NATSAckWait != 5*time.Second {
		t.Fatalf("NATSAckWait = %s, want 5s", cfg.NATSAckWait)
	}
	if cfg.NATSMaxDeliveries != 5 {
		t.Fatalf("NATSMaxDeliveries = %d, want 5", cfg.NATSMaxDeliveries)
	}
	if cfg.NATSDLQSubject != "events_dlq.notifications" {
		t.Fatalf("NATSDLQSubject = %q, want events_dlq.notifications", cfg.NATSDLQSubject)
	}
	if cfg.ResendMaxWait != 200*time.Millisecond {
		t.Fatalf("ResendMaxWait = %s, want 200ms", cfg.ResendMaxWait)
	}
	if cfg.JobInsertBatchSize != 500 {
		t.Fatalf("JobInsertBatchSize = %d, want 500", cfg.JobInsertBatchSize)
	}
	if cfg.JobCleanupInterval != 30*time.Minute {
		t.Fatalf("JobCleanupInterval = %s, want 30m", cfg.JobCleanupInterval)
	}
	if cfg.JobRetention != 7*24*time.Hour {
		t.Fatalf("JobRetention = %s, want 168h", cfg.JobRetention)
	}
}

func TestLoadNotificationsConfig_UsesEnvOverrides(t *testing.T) {
	setRequiredNotificationsEnv(t)

	t.Setenv("HTTP_PORT", "8082")
	t.Setenv("NATS_STREAM_NAME", "DOMAIN")
	t.Setenv("NATS_SUBJECT_PREFIX", "domain_events")
	t.Setenv("NATS_CONSUMER_NAME", "email-workers")
	t.Setenv("NATS_BATCH_SIZE", "64")
	t.Setenv("NATS_ACK_WAIT", "2s")
	t.Setenv("NATS_MAX_DELIVERIES", "9")
	t.Setenv("NATS_DLQ_SUBJECT", "domain_events_dlq.notifications")
	t.Setenv("LOG_LEVEL", "debug")
	t.Setenv("SERVICE_NAME", "emails")
	t.Setenv("ENVIRONMENT", "prod")
	t.Setenv("RESEND_MAX_WAIT", "500ms")
	t.Setenv("NOTIFICATION_JOB_INSERT_BATCH_SIZE", "250")
	t.Setenv("NOTIFICATION_JOB_CLEANUP_INTERVAL", "15m")
	t.Setenv("NOTIFICATION_JOB_RETENTION", "48h")

	cfg, err := LoadNotificationsConfig()
	if err != nil {
		t.Fatalf("LoadNotificationsConfig returned error: %v", err)
	}

	if cfg.HTTPPort != "8082" {
		t.Fatalf("HTTPPort = %q, want 8082", cfg.HTTPPort)
	}
	if cfg.NATSStreamName != "DOMAIN" {
		t.Fatalf("NATSStreamName = %q, want DOMAIN", cfg.NATSStreamName)
	}
	if cfg.NATSSubjectPrefix != "domain_events" {
		t.Fatalf("NATSSubjectPrefix = %q, want domain_events", cfg.NATSSubjectPrefix)
	}
	if cfg.NATSConsumerName != "email-workers" {
		t.Fatalf("NATSConsumerName = %q, want email-workers", cfg.NATSConsumerName)
	}
	if cfg.NATSBatchSize != 64 {
		t.Fatalf("NATSBatchSize = %d, want 64", cfg.NATSBatchSize)
	}
	if cfg.NATSAckWait != 2*time.Second {
		t.Fatalf("NATSAckWait = %s, want 2s", cfg.NATSAckWait)
	}
	if cfg.NATSMaxDeliveries != 9 {
		t.Fatalf("NATSMaxDeliveries = %d, want 9", cfg.NATSMaxDeliveries)
	}
	if cfg.NATSDLQSubject != "domain_events_dlq.notifications" {
		t.Fatalf("NATSDLQSubject = %q, want domain_events_dlq.notifications", cfg.NATSDLQSubject)
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
	if cfg.JobInsertBatchSize != 250 {
		t.Fatalf("JobInsertBatchSize = %d, want 250", cfg.JobInsertBatchSize)
	}
	if cfg.JobCleanupInterval != 15*time.Minute {
		t.Fatalf("JobCleanupInterval = %s, want 15m", cfg.JobCleanupInterval)
	}
	if cfg.JobRetention != 48*time.Hour {
		t.Fatalf("JobRetention = %s, want 48h", cfg.JobRetention)
	}
}

func TestLoadNotificationsConfig_MissingRequiredEnvReturnsError(t *testing.T) {
	required := []string{
		"DATABASE_URL",
		"NATS_URL",
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

	t.Setenv("NATS_BATCH_SIZE", "many")
	t.Setenv("NATS_ACK_WAIT", "fast")
	t.Setenv("NATS_MAX_DELIVERIES", "many")
	t.Setenv("RESEND_MAX_WAIT", "soon")
	t.Setenv("NOTIFICATION_JOB_INSERT_BATCH_SIZE", "many")
	t.Setenv("NOTIFICATION_JOB_CLEANUP_INTERVAL", "often")
	t.Setenv("NOTIFICATION_JOB_RETENTION", "long")

	cfg, err := LoadNotificationsConfig()
	if err != nil {
		t.Fatalf("LoadNotificationsConfig returned error: %v", err)
	}

	if cfg.NATSBatchSize != 32 {
		t.Fatalf("NATSBatchSize = %d, want 32", cfg.NATSBatchSize)
	}
	if cfg.NATSAckWait != 5*time.Second {
		t.Fatalf("NATSAckWait = %s, want 5s", cfg.NATSAckWait)
	}
	if cfg.NATSMaxDeliveries != 5 {
		t.Fatalf("NATSMaxDeliveries = %d, want 5", cfg.NATSMaxDeliveries)
	}
	if cfg.ResendMaxWait != 200*time.Millisecond {
		t.Fatalf("ResendMaxWait = %s, want 200ms", cfg.ResendMaxWait)
	}
	if cfg.JobInsertBatchSize != 500 {
		t.Fatalf("JobInsertBatchSize = %d, want 500", cfg.JobInsertBatchSize)
	}
	if cfg.JobCleanupInterval != 30*time.Minute {
		t.Fatalf("JobCleanupInterval = %s, want 30m", cfg.JobCleanupInterval)
	}
	if cfg.JobRetention != 7*24*time.Hour {
		t.Fatalf("JobRetention = %s, want 168h", cfg.JobRetention)
	}
}

func TestLoadNotificationsConfig_NegativeMaxDeliveriesFallsBackToDefault(t *testing.T) {
	setRequiredNotificationsEnv(t)
	clearOptionalNotificationsEnv(t)

	t.Setenv("NATS_MAX_DELIVERIES", "-1")

	cfg, err := LoadNotificationsConfig()
	if err != nil {
		t.Fatalf("LoadNotificationsConfig returned error: %v", err)
	}

	if cfg.NATSMaxDeliveries != 5 {
		t.Fatalf("NATSMaxDeliveries = %d, want 5", cfg.NATSMaxDeliveries)
	}
}

func TestLoadNotificationsConfig_NonPositiveOptionalValuesFallBackToDefaults(t *testing.T) {
	setRequiredNotificationsEnv(t)

	t.Setenv("NATS_BATCH_SIZE", "0")
	t.Setenv("NATS_ACK_WAIT", "0s")
	t.Setenv("NATS_MAX_DELIVERIES", "0")
	t.Setenv("RESEND_MAX_WAIT", "-1s")
	t.Setenv("NOTIFICATION_JOB_INSERT_BATCH_SIZE", "0")
	t.Setenv("NOTIFICATION_JOB_CLEANUP_INTERVAL", "0s")
	t.Setenv("NOTIFICATION_JOB_RETENTION", "-1h")

	cfg, err := LoadNotificationsConfig()
	if err != nil {
		t.Fatalf("LoadNotificationsConfig returned error: %v", err)
	}

	if cfg.NATSBatchSize != 32 {
		t.Fatalf("NATSBatchSize = %d, want 32", cfg.NATSBatchSize)
	}
	if cfg.NATSAckWait != 5*time.Second {
		t.Fatalf("NATSAckWait = %s, want 5s", cfg.NATSAckWait)
	}
	if cfg.NATSMaxDeliveries != 0 {
		t.Fatalf("NATSMaxDeliveries = %d, want 0", cfg.NATSMaxDeliveries)
	}
	if cfg.ResendMaxWait != 200*time.Millisecond {
		t.Fatalf("ResendMaxWait = %s, want 200ms", cfg.ResendMaxWait)
	}
	if cfg.JobInsertBatchSize != 500 {
		t.Fatalf("JobInsertBatchSize = %d, want 500", cfg.JobInsertBatchSize)
	}
	if cfg.JobCleanupInterval != 30*time.Minute {
		t.Fatalf("JobCleanupInterval = %s, want 30m", cfg.JobCleanupInterval)
	}
	if cfg.JobRetention != 7*24*time.Hour {
		t.Fatalf("JobRetention = %s, want 168h", cfg.JobRetention)
	}
}
