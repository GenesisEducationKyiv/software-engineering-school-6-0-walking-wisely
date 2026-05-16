// Package postgres initializes PostgreSQL infrastructure clients.
package postgres

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/config"
)

// InitDB opens a PostgreSQL connection pool using retry defaults read from the environment.
func InitDB(databaseURL string) (*pgxpool.Pool, error) {
	return InitDBWithRetry(databaseURL, config.DBRetryConfigFromEnv())
}

// InitDBWithRetry opens a PostgreSQL connection pool, retrying on transient failures according to retry.
func InitDBWithRetry(databaseURL string, retry config.RetryConfig) (*pgxpool.Pool, error) {
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
