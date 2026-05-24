//go:build integration

package postgres

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/config"
)

func TestInitDBWithRetry_ConnectsToPostgres(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
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
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})

	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("build postgres connection string: %v", err)
	}

	pool, err := InitDBWithRetry(databaseURL, config.RetryConfig{
		MaxAttempts: 1,
		InitialWait: time.Millisecond,
		MaxWait:     time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	var value int
	if err := pool.QueryRow(ctx, "select 1").Scan(&value); err != nil {
		t.Fatalf("query postgres: %v", err)
	}
	if value != 1 {
		t.Fatalf("query result = %d, want 1", value)
	}
}

func TestInitDBWithRetry_ReturnsErrorWhenPostgresUnavailable(t *testing.T) {
	hostPort := freeTCPPort(t)
	databaseURL := fmt.Sprintf("postgres://app:secret@127.0.0.1:%d/app?sslmode=disable&connect_timeout=1", hostPort)

	pool, err := InitDBWithRetry(databaseURL, config.RetryConfig{
		MaxAttempts: 1,
		InitialWait: time.Millisecond,
		MaxWait:     time.Millisecond,
	}, nil)
	if err == nil {
		pool.Close()
		t.Fatal("expected connection error")
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free TCP port: %v", err)
	}
	defer listener.Close()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split listener address: %v", err)
	}

	var parsed int
	if _, err := fmt.Sscanf(port, "%d", &parsed); err != nil {
		t.Fatalf("parse free TCP port %q: %v", port, err)
	}
	return parsed
}
