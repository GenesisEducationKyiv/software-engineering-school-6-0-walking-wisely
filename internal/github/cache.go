package github

import (
	"context"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

// ReleaseCacheTTL is the default duration for cached GitHub release responses.
const ReleaseCacheTTL = 10 * time.Minute

// ReleaseClient fetches release information for a repository.
type ReleaseClient interface {
	GetLatestRelease(ctx context.Context, repo string) (*Release, error)
}

// ReleaseCache stores latest-release responses outside the GitHub API client.
type ReleaseCache interface {
	GetRelease(ctx context.Context, repo string) (*Release, bool, error)
	SetRelease(ctx context.Context, repo string, release *Release, ttl time.Duration) error
}

// CachedReleaseClient adds cache-aside behavior to a ReleaseClient.
type CachedReleaseClient struct {
	next  ReleaseClient
	cache ReleaseCache
	ttl   time.Duration
	log   logger.Logger
}

// NewCachedReleaseClient returns a ReleaseClient that reads from cache before
// falling back to next. Cache write failures are logged and do not fail the call.
func NewCachedReleaseClient(next ReleaseClient, cache ReleaseCache, ttl time.Duration, log logger.Logger) *CachedReleaseClient {
	if log == nil {
		log = logger.NoopLogger{}
	}
	return &CachedReleaseClient{next: next, cache: cache, ttl: ttl, log: log}
}

// GetLatestRelease returns the latest release, preferring a cached value when available.
func (c *CachedReleaseClient) GetLatestRelease(ctx context.Context, repo string) (*Release, error) {
	if release, ok, err := c.cache.GetRelease(ctx, repo); err == nil && ok {
		c.log.Debug("github release cache hit", "repo", repo)
		return release, nil
	} else if err != nil {
		c.log.Warn("github release cache read failed", "repo", repo, "err", err)
	}

	release, err := c.next.GetLatestRelease(ctx, repo)
	if err != nil {
		return nil, err
	}

	if err := c.cache.SetRelease(ctx, repo, release, c.ttl); err != nil {
		c.log.Warn("failed to cache github release", "repo", repo, "err", err)
	}
	return release, nil
}
