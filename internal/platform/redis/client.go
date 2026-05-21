// Package redis initializes Redis infrastructure clients.
package redis

import (
	"context"
	"fmt"
	"math"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/config"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

// NewClient creates a Redis client and pings it, using retry defaults read from the environment.
func NewClient(redisURL string, log logger.Logger) (*goredis.Client, error) {
	return NewClientWithRetry(redisURL, config.RedisRetryConfigFromEnv(), log)
}

// NewClientWithRetry creates a Redis client and pings it, retrying on transient failures according to retry.
func NewClientWithRetry(redisURL string, retry config.RetryConfig, log logger.Logger) (*goredis.Client, error) {
	if log == nil {
		log = logger.NoopLogger{}
	}

	opts, err := goredis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse Redis URL: %w", err)
	}

	client := goredis.NewClient(opts)

	var lastErr error
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		err := client.Ping(ctx).Err()
		cancel()

		if err == nil {
			log.Info("connected to Redis")
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
		log.Warn("Redis connection attempt failed, retrying",
			"attempt", attempt, "max_attempts", retry.MaxAttempts,
			"err", err, "retry_in", wait)
		time.Sleep(wait)
	}

	if err = client.Close(); err != nil {
		return nil, fmt.Errorf("failed to connect to Redis after %d attempts: %w; also failed to close client: %w", retry.MaxAttempts, lastErr, err)
	}
	return nil, fmt.Errorf("failed to connect to Redis after %d attempts: %w", retry.MaxAttempts, lastErr)
}
