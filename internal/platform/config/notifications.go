package config

import (
	"fmt"
	"os"
	"time"
)

// NotificationsConfig holds all environment-driven configuration for the notifications service.
type NotificationsConfig struct {
	HTTPPort        string
	DatabaseURL     string
	RedisURL        string
	ResendAPIKey    string
	FromEmail       string
	BaseURL         string
	StreamKey       string
	StreamGroup     string
	StreamBatchSize int
	LogLevel        string
	ServiceName     string
	Environment     string
	ResendMaxWait   time.Duration
}

// LoadNotificationsConfig reads configuration from environment variables.
func LoadNotificationsConfig() (*NotificationsConfig, error) {
	cfg := &NotificationsConfig{
		HTTPPort:        envOrDefault("HTTP_PORT", "8081"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		RedisURL:        os.Getenv("REDIS_URL"),
		ResendAPIKey:    os.Getenv("RESEND_API_KEY"),
		FromEmail:       os.Getenv("FROM_EMAIL"),
		BaseURL:         os.Getenv("BASE_URL"),
		StreamKey:       envOrDefault("STREAM_KEY", "events"),
		StreamGroup:     envOrDefault("STREAM_GROUP", "notifications"),
		StreamBatchSize: parseIntOrDefault("STREAM_BATCH_SIZE", 32),
		LogLevel:        envOrDefault("LOG_LEVEL", "info"),
		ServiceName:     envOrDefault("SERVICE_NAME", "notifications"),
		Environment:     envOrDefault("ENVIRONMENT", "local"),
		ResendMaxWait:   parseDurationOrDefault("RESEND_MAX_WAIT", 200*time.Millisecond),
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *NotificationsConfig) validate() error {
	required := []struct{ key, val string }{
		{"DATABASE_URL", c.DatabaseURL},
		{"REDIS_URL", c.RedisURL},
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
