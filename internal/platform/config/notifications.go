package config

import (
	"fmt"
	"os"
	"time"
)

// NotificationsConfig holds all environment-driven configuration for the notifications service.
type NotificationsConfig struct {
	HTTPPort                     string
	DatabaseURL                  string
	NATSURL                      string
	ResendAPIKey                 string
	FromEmail                    string
	BaseURL                      string
	NATSStreamName               string
	NATSSubjectPrefix            string
	NATSConsumerName             string
	NATSBatchSize                int
	NATSAckWait                  time.Duration
	NATSMaxDeliveries            int
	NATSDLQSubject               string
	LogLevel                     string
	ServiceName                  string
	Environment                  string
	ResendMaxWait                time.Duration
	JobInsertBatchSize           int
	JobCleanupInterval           time.Duration
	JobRetention                 time.Duration
	NotificationsOutboxInterval  time.Duration
	NotificationsOutboxRetention time.Duration
	GRPCPort                     string // port for the internal notification gRPC server
}

// LoadNotificationsConfig reads configuration from environment variables.
func LoadNotificationsConfig() (*NotificationsConfig, error) {
	cfg := &NotificationsConfig{
		HTTPPort:                     envOrDefault("HTTP_PORT", "8081"),
		DatabaseURL:                  os.Getenv("DATABASE_URL"),
		NATSURL:                      os.Getenv("NATS_URL"),
		ResendAPIKey:                 os.Getenv("RESEND_API_KEY"),
		FromEmail:                    os.Getenv("FROM_EMAIL"),
		BaseURL:                      os.Getenv("BASE_URL"),
		NATSStreamName:               envOrDefault("NATS_STREAM_NAME", "EVENTS"),
		NATSSubjectPrefix:            envOrDefault("NATS_SUBJECT_PREFIX", "events"),
		NATSConsumerName:             envOrDefault("NATS_CONSUMER_NAME", "notifications"),
		NATSBatchSize:                parseIntOrDefault("NATS_BATCH_SIZE", 32),
		NATSAckWait:                  parseDurationOrDefault("NATS_ACK_WAIT", 5*time.Second),
		NATSMaxDeliveries:            int(parseNonNegativeInt64OrDefault("NATS_MAX_DELIVERIES", 5)),
		NATSDLQSubject:               envOrDefault("NATS_DLQ_SUBJECT", "events_dlq.notifications"),
		LogLevel:                     envOrDefault("LOG_LEVEL", "info"),
		ServiceName:                  envOrDefault("SERVICE_NAME", "notifications"),
		Environment:                  envOrDefault("ENVIRONMENT", "local"),
		ResendMaxWait:                parseDurationOrDefault("RESEND_MAX_WAIT", 200*time.Millisecond),
		JobInsertBatchSize:           parseIntOrDefault("NOTIFICATION_JOB_INSERT_BATCH_SIZE", 500),
		JobCleanupInterval:           parseDurationOrDefault("NOTIFICATION_JOB_CLEANUP_INTERVAL", 30*time.Minute),
		JobRetention:                 parseDurationOrDefault("NOTIFICATION_JOB_RETENTION", 7*24*time.Hour),
		NotificationsOutboxInterval:  parseDurationOrDefault("NOTIFICATIONS_OUTBOX_INTERVAL", 200*time.Millisecond),
		NotificationsOutboxRetention: parseDurationOrDefault("NOTIFICATIONS_OUTBOX_RETENTION", 7*24*time.Hour),
		GRPCPort:                     envOrDefault("GRPC_PORT", "9091"),
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *NotificationsConfig) validate() error {
	required := []struct{ key, val string }{
		{"DATABASE_URL", c.DatabaseURL},
		{"NATS_URL", c.NATSURL},
		{"RESEND_API_KEY", c.ResendAPIKey},
		{"FROM_EMAIL", c.FromEmail},
		{"BASE_URL", c.BaseURL},
	}
	for _, r := range required {
		if r.val == "" {
			return fmt.Errorf("required env var %s is not set", r.key)
		}
	}
	return nil
}
