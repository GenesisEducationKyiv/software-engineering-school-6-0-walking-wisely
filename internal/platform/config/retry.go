package config

import (
	"os"
	"strconv"
	"time"
)

// RetryConfig controls the exponential-backoff retry behaviour for infrastructure connections.
type RetryConfig struct {
	MaxAttempts int
	InitialWait time.Duration
	MaxWait     time.Duration
}

func retryConfigFromEnv(prefix string, defaultMax int, defaultInitial, defaultMaxWait time.Duration) RetryConfig {
	cfg := RetryConfig{
		MaxAttempts: defaultMax,
		InitialWait: defaultInitial,
		MaxWait:     defaultMaxWait,
	}
	if v := os.Getenv(prefix + "_MAX_ATTEMPTS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxAttempts = n
		}
	}
	if v := os.Getenv(prefix + "_INITIAL_WAIT_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.InitialWait = time.Duration(n) * time.Millisecond
		}
	}
	if v := os.Getenv(prefix + "_MAX_WAIT_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.MaxWait = time.Duration(n) * time.Millisecond
		}
	}
	return cfg
}

// DBRetryConfigFromEnv reads DB_RETRY_* environment variables and returns a RetryConfig for Postgres.
func DBRetryConfigFromEnv() RetryConfig {
	return retryConfigFromEnv("DB_RETRY", 5, 500*time.Millisecond, 30*time.Second)
}

// RedisRetryConfigFromEnv reads REDIS_RETRY_* environment variables and returns a RetryConfig for Redis.
func RedisRetryConfigFromEnv() RetryConfig {
	return retryConfigFromEnv("REDIS_RETRY", 5, 500*time.Millisecond, 30*time.Second)
}
