// Package clients provides HTTP clients for the GitHub REST API and the Resend email API.
package clients

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/domain"
)

const githubReleaseCacheTTL = 10 * time.Minute

// Release holds the fields we care about from a GitHub release object.
type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Name    string `json:"name"`
}

// GitHubClient wraps the GitHub REST API with Redis-backed caching and
// transparent rate-limit error propagation.
type GitHubClient struct {
	http  *http.Client
	redis *redis.Client
	token string // optional Bearer token for higher rate limits
}

// NewGitHubClient returns a GitHubClient backed by redisClient for caching. githubToken is optional
// but raises the GitHub API rate limit from 60 to 5 000 requests per hour when provided.
func NewGitHubClient(redisClient *redis.Client, githubToken string) *GitHubClient {
	return &GitHubClient{
		http:  &http.Client{Timeout: 10 * time.Second},
		redis: redisClient,
		token: githubToken,
	}
}

// GetLatestRelease returns the latest release for a repo in "owner/repo" format.
// Results are cached in Redis for githubReleaseCacheTTL (10 minutes).
// Returns domain.ErrRepoNotFound on 404, or *domain.RateLimitError on 429/403.
func (c *GitHubClient) GetLatestRelease(ctx context.Context, repo string) (*Release, error) {
	cacheKey := "github:release:" + repo

	if cached, err := c.redis.Get(ctx, cacheKey).Bytes(); err == nil {
		var r Release
		if err := json.Unmarshal(cached, &r); err == nil {
			slog.Debug("github release cache hit", "repo", repo)
			return &r, nil
		}
	}

	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, name)
	release, err := c.doRequest(ctx, url, repo)
	if err != nil {
		return nil, err
	}

	if data, err := json.Marshal(release); err == nil {
		if err := c.redis.Set(ctx, cacheKey, data, githubReleaseCacheTTL).Err(); err != nil {
			slog.Warn("failed to cache github release", "repo", repo, "err", err)
		}
	}

	slog.Debug("github release fetched", "repo", repo, "tag", release.TagName)
	return release, nil
}

// ValidateRepo confirms that the repository exists on GitHub. Used during
// subscription to give the user an immediate 404 rather than a silent failure.
func (c *GitHubClient) ValidateRepo(ctx context.Context, repo string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, name)
	_, err = c.doRequest(ctx, url, repo)
	return err
}

// doRequest executes a GET against url and maps GitHub's status codes to domain errors.
func (c *GitHubClient) doRequest(ctx context.Context, url, repo string) (release *Release, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, http.NoBody)
	if err != nil {
		return nil, fmt.Errorf("build github request: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("github request: %w", err)
	}

	defer func() {
		closeErr := resp.Body.Close()
		if err != nil {
			slog.Warn("close github response body", "err", closeErr)
			err = closeErr
		}
	}()

	switch resp.StatusCode {
	case http.StatusOK:
		var r Release
		if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
			return nil, fmt.Errorf("decode github response: %w", err)
		}
		return &r, nil

	case http.StatusNotFound:
		return nil, domain.ErrRepoNotFound

	case http.StatusTooManyRequests, http.StatusForbidden:
		// GitHub uses 403 + X-RateLimit-* for primary rate limits and
		// 429 + Retry-After for secondary rate limits.
		retryAfter := parseRetryAfter(resp)
		slog.Warn("github rate limited", "repo", repo, "retry_after", retryAfter)
		return nil, &domain.RateLimitError{Service: "GitHub", RetryAfter: retryAfter}

	default:
		return nil, fmt.Errorf("github API unexpected status %d for %s", resp.StatusCode, repo)
	}
}

func splitRepo(repo string) (owner, name string, err error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo format %q, expected owner/repo", repo)
	}
	return parts[0], parts[1], nil
}

// parseRetryAfter reads the Retry-After header (seconds) or falls back to
// X-RateLimit-Reset (Unix timestamp), which GitHub uses for primary limits.
func parseRetryAfter(resp *http.Response) time.Duration {
	if v := resp.Header.Get("Retry-After"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	if v := resp.Header.Get("X-RateLimit-Reset"); v != "" {
		if unix, err := strconv.ParseInt(v, 10, 64); err == nil {
			if d := time.Until(time.Unix(unix, 0)); d > 0 {
				return d
			}
		}
	}
	return 60 * time.Second // conservative fallback
}
