// Package config handles environment variable loading and infrastructure client initialisation with retry.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// AppConfig holds all environment-driven configuration for the API service.
type AppConfig struct {
	RestPort        string
	GrpcPort        string
	DatabaseURL     string
	RedisURL        string
	EmailSecretKey  string
	GithubToken     string // optional - raises GitHub API rate limit
	StreamKey       string // Redis Stream key for publishing domain events
	StreamMaxLen    int64  // Redis Stream retention max length; 0 disables trimming
	LogLevel        string
	ServiceName     string
	Environment     string
	ScannerInterval time.Duration
}

// LoadAppConfig reads all configuration from environment variables and returns a validated AppConfig.
func LoadAppConfig() (*AppConfig, error) {
	cfg := &AppConfig{
		RestPort:        envOrDefault("REST_PORT", "8080"),
		GrpcPort:        envOrDefault("GRPC_PORT", "9090"),
		DatabaseURL:     os.Getenv("DATABASE_URL"),
		RedisURL:        os.Getenv("REDIS_URL"),
		EmailSecretKey:  os.Getenv("EMAIL_SECRET_KEY"),
		GithubToken:     os.Getenv("GITHUB_TOKEN"),
		StreamKey:       envOrDefault("STREAM_KEY", "events"),
		StreamMaxLen:    parseNonNegativeInt64OrDefault("STREAM_MAX_LEN", 100_000),
		LogLevel:        envOrDefault("LOG_LEVEL", "info"),
		ServiceName:     envOrDefault("SERVICE_NAME", "github-release-notifier"),
		Environment:     envOrDefault("ENVIRONMENT", "local"),
		ScannerInterval: parseDurationOrDefault("SCANNER_INTERVAL", 5*time.Minute),
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
		{"EMAIL_SECRET_KEY", c.EmailSecretKey},
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

func parseNonNegativeInt64OrDefault(key string, def int64) int64 {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			return n
		}
	}
	return def
}
