//go:build integration

package redis

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	platformconfig "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/config"
)

func TestIntegration_NewClientWithRetry_ConnectsToRedis(t *testing.T) {
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := testcontainers.Run(
		ctx,
		"redis:7-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("6379/tcp")),
	)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate redis container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("get redis host: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("get redis port: %v", err)
	}
	redisURL := "redis://" + net.JoinHostPort(host, port.Port()) + "/0"

	client, err := NewClientWithRetry(redisURL, platformconfig.RetryConfig{
		MaxAttempts: 1,
		InitialWait: time.Millisecond,
		MaxWait:     time.Millisecond,
	}, nil)
	if err != nil {
		t.Fatalf("connect to redis: %v", err)
	}
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Logf("close redis client: %v", err)
		}
	})

	if err := client.Set(ctx, "healthcheck", "ok", 0).Err(); err != nil {
		t.Fatalf("set redis key: %v", err)
	}
	value, err := client.Get(ctx, "healthcheck").Result()
	if err != nil {
		t.Fatalf("get redis key: %v", err)
	}
	if value != "ok" {
		t.Fatalf("redis value = %q, want ok", value)
	}
}

func TestIntegration_NewClientWithRetry_ReturnsErrorWhenRedisUnavailable(t *testing.T) {
	hostPort := freeTCPPort(t)
	redisURL := fmt.Sprintf("redis://127.0.0.1:%d/0", hostPort)

	client, err := NewClientWithRetry(redisURL, platformconfig.RetryConfig{
		MaxAttempts: 1,
		InitialWait: time.Millisecond,
		MaxWait:     time.Millisecond,
	}, nil)
	if err == nil {
		if closeErr := client.Close(); closeErr != nil {
			t.Logf("close redis client: %v", closeErr)
		}
		t.Fatal("expected connection error")
	}
}

func freeTCPPort(t *testing.T) int {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("find free TCP port: %v", err)
	}
	defer listener.Close() //nolint:errcheck // error from Close in defer is not actionable

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
