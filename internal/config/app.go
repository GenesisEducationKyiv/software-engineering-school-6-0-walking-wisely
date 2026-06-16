// Package config handles environment variable loading and infrastructure client initialisation with retry.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// AppConfig holds all environment-driven configuration for the application.
type AppConfig struct {
	RestPort         string
	GrpcPort         string
	DatabaseURL      string
	RedisURL         string
	ResendAPIKey     string
	EmailSecretKey   string
	GithubToken      string // optional - raises GitHub API rate limit
	BaseURL          string // used to build confirm/unsubscribe links in emails
	FromEmail        string
	LogLevel         string
	ServiceName      string
	Environment      string
	ScannerInterval  time.Duration
	ResendMaxWait    time.Duration
	EmailChannelSize int
}

// LoadAppConfig reads all configuration from environment variables and returns a validated AppConfig.
func LoadAppConfig() (*AppConfig, error) {
	cfg := &AppConfig{
		RestPort:         envOrDefault("REST_PORT", "8080"),
		GrpcPort:         envOrDefault("GRPC_PORT", "9090"),
		DatabaseURL:      os.Getenv("DATABASE_URL"),
		RedisURL:         os.Getenv("REDIS_URL"),
		ResendAPIKey:     os.Getenv("RESEND_API_KEY"),
		EmailSecretKey:   os.Getenv("EMAIL_SECRET_KEY"),
		GithubToken:      os.Getenv("GITHUB_TOKEN"),
		BaseURL:          os.Getenv("BASE_URL"),
		FromEmail:        os.Getenv("FROM_EMAIL"),
		LogLevel:         envOrDefault("LOG_LEVEL", "info"),
		ServiceName:      envOrDefault("SERVICE_NAME", "github-release-notifier"),
		Environment:      envOrDefault("ENVIRONMENT", "local"),
		ScannerInterval:  parseDurationOrDefault("SCANNER_INTERVAL", 5*time.Minute),
		ResendMaxWait:    parseDurationOrDefault("RESEND_MAX_WAIT", 200*time.Millisecond),
		EmailChannelSize: parseIntOrDefault("EMAIL_CHANNEL_SIZE", 1000),
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func (c *AppConfig) validate() error {
	required := []struct{ key, val string }{
		{"DATABASE_URL", c.DatabaseURL},
		{"REDIS_URL", c.RedisURL},
		{"RESEND_API_KEY", c.ResendAPIKey},
		{"EMAIL_SECRET_KEY", c.EmailSecretKey},
		{"BASE_URL", c.BaseURL},
		{"FROM_EMAIL", c.FromEmail},
	}
	for _, r := range required {
		if r.val == "" {
			return fmt.Errorf("required env var %s is not set", r.key)
		}
	}
	return nil
}

func envOrDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func parseDurationOrDefault(key string, def time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return def
}

func parseIntOrDefault(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return def
}
