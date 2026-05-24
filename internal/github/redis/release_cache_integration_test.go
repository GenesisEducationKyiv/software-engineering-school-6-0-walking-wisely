//go:build integration

package redis

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/github"
)

func TestIntegration_GitHubReleaseCache_SetThenGetRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := newTestRedisClient(t, ctx)
	cache := NewGitHubReleaseCache(client)
	repo := "owner/repo"
	want := &github.Release{
		TagName: "v1.2.3",
		HTMLURL: "https://github.com/owner/repo/releases/tag/v1.2.3",
		Name:    "Release 1.2.3",
	}

	if err := cache.SetRelease(ctx, repo, want, time.Minute); err != nil {
		t.Fatalf("SetRelease returned error: %v", err)
	}

	got, ok, err := cache.GetRelease(ctx, repo)
	if err != nil {
		t.Fatalf("GetRelease returned error: %v", err)
	}
	if !ok {
		t.Fatal("GetRelease ok = false, want true")
	}
	if *got != *want {
		t.Fatalf("release = %#v, want %#v", got, want)
	}

	ttl, err := client.TTL(ctx, releaseKey(repo)).Result()
	if err != nil {
		t.Fatalf("get release key TTL: %v", err)
	}
	if ttl <= 0 {
		t.Fatalf("TTL = %s, want positive duration", ttl)
	}
}

func TestIntegration_GitHubReleaseCache_GetRelease_MissingKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := newTestRedisClient(t, ctx)
	cache := NewGitHubReleaseCache(client)

	release, ok, err := cache.GetRelease(ctx, "owner/repo")
	if err != nil {
		t.Fatalf("GetRelease returned error: %v", err)
	}
	if ok {
		t.Fatal("GetRelease ok = true, want false")
	}
	if release != nil {
		t.Fatalf("release = %#v, want nil", release)
	}
}

func TestIntegration_GitHubReleaseCache_GetRelease_InvalidJSON(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := newTestRedisClient(t, ctx)
	cache := NewGitHubReleaseCache(client)
	repo := "owner/repo"

	if err := client.Set(ctx, releaseKey(repo), "{", time.Minute).Err(); err != nil {
		t.Fatalf("set invalid cached release: %v", err)
	}

	release, ok, err := cache.GetRelease(ctx, repo)
	if err == nil {
		t.Fatal("GetRelease error = nil, want decode error")
	}
	if !strings.Contains(err.Error(), "decode cached github release") {
		t.Fatalf("error = %v, want decode cached github release", err)
	}
	if ok {
		t.Fatal("GetRelease ok = true, want false")
	}
	if release != nil {
		t.Fatalf("release = %#v, want nil", release)
	}
}

func TestIntegration_GitHubReleaseCache_GetRelease_ExpiredKey(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	client := newTestRedisClient(t, ctx)
	cache := NewGitHubReleaseCache(client)
	repo := "owner/repo"

	if err := cache.SetRelease(ctx, repo, &github.Release{TagName: "v1.2.3"}, time.Minute); err != nil {
		t.Fatalf("SetRelease returned error: %v", err)
	}
	expired, err := client.Expire(ctx, releaseKey(repo), 0).Result()
	if err != nil {
		t.Fatalf("expire release key: %v", err)
	}
	if !expired {
		t.Fatal("expire release key = false, want true")
	}

	release, ok, err := cache.GetRelease(ctx, repo)
	if err != nil {
		t.Fatalf("GetRelease returned error: %v", err)
	}
	if ok {
		t.Fatal("GetRelease ok = true, want false")
	}
	if release != nil {
		t.Fatalf("release = %#v, want nil", release)
	}
}

func newTestRedisClient(t *testing.T, ctx context.Context) *goredis.Client {
	t.Helper()

	testcontainers.SkipIfProviderIsNotHealthy(t)

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

	client := goredis.NewClient(&goredis.Options{
		Addr: net.JoinHostPort(host, port.Port()),
	})
	t.Cleanup(func() {
		if err := client.Close(); err != nil {
			t.Logf("close redis client: %v", err)
		}
	})

	if err := client.Ping(ctx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}

	return client
}
