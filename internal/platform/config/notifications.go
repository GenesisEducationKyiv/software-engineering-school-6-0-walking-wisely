package config

import (
	"fmt"
	"os"
	"time"
)

// NATSConfig holds NATS configuration shared by all services.
type NATSConfig struct {
	URL           string
	StreamName    string
	SubjectPrefix string
	ConsumerName  string
	BatchSize     int
	AckWait       time.Duration
	MaxDeliveries int
	DLQSubject    string
}

// ResendConfig holds Resend email client configuration.
type ResendConfig struct {
	APIKey  string
	From    string
	BaseURL string
	MaxWait time.Duration
}

// JobConfig holds notification job worker configuration.
type JobConfig struct {
	InsertBatchSize int
	CleanupInterval time.Duration
	Retention       time.Duration
}

// NotificationsConfig holds all environment-driven configuration for the notifications service.
type NotificationsConfig struct {
	HTTPPort    string
	DatabaseURL string
	LogLevel    string
	ServiceName string
	Environment string
	NATS        NATSConfig
	Resend      ResendConfig
	Job         JobConfig
	Outbox      OutboxConfig // for the notifications_outbox saga-reply pipeline
	GRPCPort    string       // port for the internal notification gRPC server
	EmailSink   string       // "noop" disables real email delivery (bench / dev)
}

// LoadNotificationsConfig reads configuration from environment variables.
func LoadNotificationsConfig() (*NotificationsConfig, error) {
	cfg := &NotificationsConfig{
		HTTPPort:    envOrDefault("HTTP_PORT", "8081"),
		DatabaseURL: os.Getenv("DATABASE_URL"),
		LogLevel:    envOrDefault("LOG_LEVEL", "info"),
		ServiceName: envOrDefault("SERVICE_NAME", "notifications"),
		Environment: envOrDefault("ENVIRONMENT", "local"),
		NATS: NATSConfig{
			URL:           os.Getenv("NATS_URL"),
			StreamName:    envOrDefault("NATS_STREAM_NAME", "EVENTS"),
			SubjectPrefix: envOrDefault("NATS_SUBJECT_PREFIX", "events"),
			ConsumerName:  envOrDefault("NATS_CONSUMER_NAME", "notifications"),
			BatchSize:     parseIntOrDefault("NATS_BATCH_SIZE", 32),
			AckWait:       parseDurationOrDefault("NATS_ACK_WAIT", 5*time.Second),
			MaxDeliveries: int(parseNonNegativeInt64OrDefault("NATS_MAX_DELIVERIES", 5)),
			DLQSubject:    envOrDefault("NATS_DLQ_SUBJECT", "events_dlq.notifications"),
		},
		Resend: ResendConfig{
			APIKey:  os.Getenv("RESEND_API_KEY"),
			From:    os.Getenv("FROM_EMAIL"),
			BaseURL: os.Getenv("BASE_URL"),
			MaxWait: parseDurationOrDefault("RESEND_MAX_WAIT", 200*time.Millisecond),
		},
		Job: JobConfig{
			InsertBatchSize: parseIntOrDefault("NOTIFICATION_JOB_INSERT_BATCH_SIZE", 500),
			CleanupInterval: parseDurationOrDefault("NOTIFICATION_JOB_CLEANUP_INTERVAL", 30*time.Minute),
			Retention:       parseDurationOrDefault("NOTIFICATION_JOB_RETENTION", 7*24*time.Hour),
		},
		Outbox: OutboxConfig{
			CleanupInterval: parseDurationOrDefault("NOTIFICATIONS_OUTBOX_INTERVAL", 200*time.Millisecond),
			Retention:       parseDurationOrDefault("NOTIFICATIONS_OUTBOX_RETENTION", 7*24*time.Hour),
		},
		GRPCPort:  envOrDefault("GRPC_PORT", "9091"),
		EmailSink: os.Getenv("EMAIL_SINK"),
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *NotificationsConfig) validate() error {
	required := []struct{ key, val string }{
		{"DATABASE_URL", c.DatabaseURL},
		{"NATS_URL", c.NATS.URL},
		{"BASE_URL", c.Resend.BaseURL},
	}
	if c.EmailSink != "noop" {
		required = append(
			required,
			struct{ key, val string }{"RESEND_API_KEY", c.Resend.APIKey},
			struct{ key, val string }{"FROM_EMAIL", c.Resend.From},
		)
	}
	for _, r := range required {
		if r.val == "" {
			return fmt.Errorf("required env var %s is not set", r.key)
		}
	}
	return nil
}
