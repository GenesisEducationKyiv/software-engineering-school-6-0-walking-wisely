//go:build integration

package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	platformmigrations "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres/migrations"
)

var sharedPool *pgxpool.Pool

func TestMain(m *testing.M) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	container, err := tcpostgres.Run(
		ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("app"),
		tcpostgres.WithUsername("app"),
		tcpostgres.WithPassword("secret"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		os.Exit(0)
	}
	defer func() {
		_ = container.Terminate(context.Background()) //nolint:contextcheck
	}()

	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		panic("build postgres connection string: " + err.Error())
	}
	if err := platformmigrations.Run(databaseURL, logger.NoopLogger{}); err != nil {
		panic("run migrations: " + err.Error())
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		panic("connect to postgres: " + err.Error())
	}
	defer pool.Close()

	sharedPool = pool
	os.Exit(m.Run())
}
