// Package postgres initializes PostgreSQL infrastructure clients.
package postgres

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/config"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

// NewDB opens a PostgreSQL connection pool using retry defaults read from the environment.
func NewDB(databaseURL string, log logger.Logger) (*pgxpool.Pool, error) {
	return NewDBWithRetry(databaseURL, config.DBRetryConfigFromEnv(), log)
}

// NewDBWithRetry opens a PostgreSQL connection pool, retrying on transient failures according to retry.
func NewDBWithRetry(databaseURL string, retry config.RetryConfig, log logger.Logger) (*pgxpool.Pool, error) {
	if log == nil {
		log = logger.NoopLogger{}
	}

	if err := validateDBRetryConfig(retry); err != nil {
		return nil, err
	}

	poolCfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return nil, fmt.Errorf("unable to parse DB config: %w", err)
	}

	return newDBPoolWithRetry(retry, log, func() (*pgxpool.Pool, error) {
		return openAndPingDBPool(poolCfg)
	}, time.Sleep)
}

func validateDBRetryConfig(retry config.RetryConfig) error {
	if retry.MaxAttempts <= 0 {
		return errors.New("database retry max attempts must be positive")
	}
	if retry.InitialWait <= 0 {
		return errors.New("database retry initial wait must be positive")
	}
	if retry.MaxWait <= 0 {
		return errors.New("database retry max wait must be positive")
	}
	return nil
}

func openAndPingDBPool(poolCfg *pgxpool.Config) (*pgxpool.Pool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, err
	}

	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return pool, nil
}

func newDBPoolWithRetry(
	retry config.RetryConfig,
	log logger.Logger,
	openAndPing func() (*pgxpool.Pool, error),
	sleep func(time.Duration),
) (*pgxpool.Pool, error) {
	var lastErr error
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		pool, err := openAndPing()
		if err == nil {
			log.Info("connected to database pool")
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
		log.Warn("database connection attempt failed, retrying",
			"attempt", attempt, "max_attempts", retry.MaxAttempts,
			"err", err, "retry_in", wait)
		sleep(wait)
	}

	return nil, fmt.Errorf("failed to connect to database after %d attempts: %w", retry.MaxAttempts, lastErr)
}
