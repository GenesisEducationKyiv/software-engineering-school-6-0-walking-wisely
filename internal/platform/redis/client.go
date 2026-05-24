// Package redis initializes Redis infrastructure clients.
package redis

import (
	"context"
	"errors"
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

	if err := validateRedisRetryConfig(retry); err != nil {
		return nil, err
	}

	opts, err := goredis.ParseURL(redisURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse Redis URL: %w", err)
	}

	return newClientWithRetry(retry, log, func() (*goredis.Client, error) {
		return openAndPingRedisClient(opts)
	}, time.Sleep)
}

func validateRedisRetryConfig(retry config.RetryConfig) error {
	if retry.MaxAttempts <= 0 {
		return errors.New("Redis retry max attempts must be positive")
	}
	if retry.InitialWait <= 0 {
		return errors.New("Redis retry initial wait must be positive")
	}
	if retry.MaxWait <= 0 {
		return errors.New("Redis retry max wait must be positive")
	}
	return nil
}

func openAndPingRedisClient(opts *goredis.Options) (*goredis.Client, error) {
	client := goredis.NewClient(opts)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := client.Ping(ctx).Err(); err != nil {
		if closeErr := client.Close(); closeErr != nil {
			return nil, fmt.Errorf("%w; also failed to close client: %w", err, closeErr)
		}
		return nil, err
	}
	return client, nil
}

func newClientWithRetry(
	retry config.RetryConfig,
	log logger.Logger,
	openAndPing func() (*goredis.Client, error),
	sleep func(time.Duration),
) (*goredis.Client, error) {
	var lastErr error
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		client, err := openAndPing()
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
		sleep(wait)
	}

	return nil, fmt.Errorf("failed to connect to Redis after %d attempts: %w", retry.MaxAttempts, lastErr)
}
