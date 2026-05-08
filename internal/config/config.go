package config

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"os"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
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

// InitDB opens a PostgreSQL connection pool using retry defaults read from the environment.
func InitDB(databaseURL string) (*pgxpool.Pool, error) {
	return InitDBWithRetry(databaseURL, DBRetryConfigFromEnv())
}

// InitDBWithRetry opens a PostgreSQL connection pool, retrying on transient failures according to retry.
func InitDBWithRetry(databaseURL string, retry RetryConfig) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse DB config: %w", err)
	}

	var lastErr error
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
		if err == nil {
			err = pool.Ping(ctx)
		}
		cancel()

		if err == nil {
			slog.Info("connected to database pool")
			return pool, nil
		}
		lastErr = err

		if attempt == retry.MaxAttempts {
			break
		}

		wait := time.Duration(float64(retry.InitialWait) * math.Pow(2, float64(attempt-1)))
		if wait > retry.MaxWait {
			wait = retry.MaxWait
		}
		slog.Warn("database connection attempt failed, retrying",
			"attempt", attempt, "max_attempts", retry.MaxAttempts,
			"err", err, "retry_in", wait)
		time.Sleep(wait)
	}

	return nil, fmt.Errorf("failed to connect to database after %d attempts: %w", retry.MaxAttempts, lastErr)
}

// InitRedis creates a Redis client and pings it, using retry defaults read from the environment.
func InitRedis(redisURL string) (*redis.Client, error) {
	return InitRedisWithRetry(redisURL, RedisRetryConfigFromEnv())
}

// InitRedisWithRetry creates a Redis client and pings it, retrying on transient failures according to retry.
func InitRedisWithRetry(redisURL string, retry RetryConfig) (*redis.Client, error) {
	opts, err := redis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse Redis URL: %w", err)
	}

	client := redis.NewClient(opts)

	var lastErr error
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := client.Ping(ctx).Err()
		cancel()

		if err == nil {
			slog.Info("connected to Redis")
			return client, nil
		}
		lastErr = err

		if attempt == retry.MaxAttempts {
			break
		}

		wait := time.Duration(float64(retry.InitialWait) * math.Pow(2, float64(attempt-1)))
		if wait > retry.MaxWait {
			wait = retry.MaxWait
		}
		slog.Warn("Redis connection attempt failed, retrying",
			"attempt", attempt, "max_attempts", retry.MaxAttempts,
			"err", err, "retry_in", wait)
		time.Sleep(wait)
	}

	if err = client.Close(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis after %d attempts: %w; also failed to close client: %w", retry.MaxAttempts, lastErr, err)
	}
	return nil, fmt.Errorf("failed to connect to Redis after %d attempts: %w", retry.MaxAttempts, lastErr)
}
