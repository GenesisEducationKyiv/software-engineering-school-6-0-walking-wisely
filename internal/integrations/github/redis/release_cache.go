// Package redis contains Redis-backed GitHub adapters.
package redis

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/integrations/github"
)

// GitHubReleaseCache stores latest GitHub release responses in Redis.
type GitHubReleaseCache struct {
	client *goredis.Client
}

// NewGitHubReleaseCache returns a Redis-backed GitHub release cache.
func NewGitHubReleaseCache(client *goredis.Client) *GitHubReleaseCache {
	return &GitHubReleaseCache{client: client}
}

// GetRelease loads a cached release for repo. The boolean return value is false
// when no usable cached value exists.
func (c *GitHubReleaseCache) GetRelease(ctx context.Context, repo string) (release *github.Release, ok bool, err error) {
	data, err := c.client.Get(ctx, releaseKey(repo)).Bytes()
	if errors.Is(err, goredis.Nil) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}

	var cachedRelease github.Release
	if err := json.Unmarshal(data, &cachedRelease); err != nil {
		return nil, false, fmt.Errorf("decode cached github release: %w", err)
	}
	return &cachedRelease, true, nil
}

// SetRelease stores release for repo with ttl.
func (c *GitHubReleaseCache) SetRelease(ctx context.Context, repo string, release *github.Release, ttl time.Duration) error {
	data, err := json.Marshal(release)
	if err != nil {
		return fmt.Errorf("encode cached github release: %w", err)
	}
	return c.client.Set(ctx, releaseKey(repo), data, ttl).Err()
}

func releaseKey(repo string) string {
	return "github:release:" + repo
}
