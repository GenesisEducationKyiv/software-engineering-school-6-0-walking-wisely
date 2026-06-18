// Package github provides access to the GitHub REST API.
package github

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	releasemonitoringdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain"
)

// Release holds the fields we care about from a GitHub release object.
type Release = releasemonitoringdomain.Release

// Client wraps the GitHub REST API and maps responses to domain errors.
type Client struct {
	http  *http.Client
	token string // optional Bearer token for higher rate limits
	log   logger.Logger
}

type rateLimitResource struct {
	Remaining int   `json:"remaining"`
	Reset     int64 `json:"reset"`
}

type rateLimitResponse struct {
	Resources struct {
		Core rateLimitResource `json:"core"`
	} `json:"resources"`
	Rate rateLimitResource `json:"rate"`
}

// Availability describes the current GitHub API state observed by this client.
type Availability struct {
	Authenticated bool
	Remaining     int
}

// NewClient returns a GitHub API client. githubToken is optional but raises the
// GitHub API rate limit from 60 to 5 000 requests per hour when provided.
func NewClient(githubToken string, log logger.Logger) *Client {
	if log == nil {
		log = logger.NoopLogger{}
	}
	return &Client{
		http:  &http.Client{Timeout: 10 * time.Second},
		token: githubToken,
		log:   log,
	}
}

// GetLatestRelease returns the latest release for a repo in "owner/repo" format.
// Returns contracts.ErrRepoNotFound on 404, or *contracts.RateLimitError on 429/403.
func (c *Client) GetLatestRelease(ctx context.Context, repo string) (*Release, error) {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return nil, err
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/%s/releases/latest", owner, name)
	resp, err := c.get(ctx, url)
	if err != nil {
		return nil, err
	}
	defer c.closeBody(resp.Body)

	if err := c.checkStatus(resp, repo); err != nil {
		return nil, err
	}

	var release Release
	if err := json.NewDecoder(resp.Body).Decode(&release); err != nil {
		return nil, fmt.Errorf("decode github release response: %w", err)
	}

	c.log.Debug("github release fetched", "repo", repo, "tag", release.TagName)
	return &release, nil
}

// ValidateRepo confirms that the repository exists on GitHub. Used during
// subscription to give the user an immediate 404 rather than a silent failure.
func (c *Client) ValidateRepo(ctx context.Context, repo string) error {
	owner, name, err := splitRepo(repo)
	if err != nil {
		return err
	}
	url := fmt.Sprintf("https://api.github.com/repos/%s/%s", owner, name)
	resp, err := c.get(ctx, url)
	if err != nil {
		return err
	}
	defer c.closeBody(resp.Body)

	return c.checkStatus(resp, repo)
}

// CheckAvailability verifies that GitHub accepts the configured credentials and
// that the current caller still has core API requests available.
func (c *Client) CheckAvailability(ctx context.Context) error {
	_, err := c.CheckAvailabilityStatus(ctx)
	return err
}

// CheckAvailabilityStatus verifies GitHub availability and returns the observed
// rate-limit state when GitHub provides it.
func (c *Client) CheckAvailabilityStatus(ctx context.Context) (Availability, error) {
	resp, err := c.get(ctx, "https://api.github.com/rate_limit")
	if err != nil {
		return Availability{Authenticated: c.token != "", Remaining: -1}, fmt.Errorf("github availability check: %w", err)
	}
	defer c.closeBody(resp.Body)
	switch resp.StatusCode {
	case http.StatusOK:
		limit, err := decodeRateLimit(resp.Body)
		if err != nil {
			return Availability{Authenticated: c.token != "", Remaining: -1}, fmt.Errorf("github availability check: %w", err)
		}
		status := Availability{
			Authenticated: c.token != "",
			Remaining:     limit.Remaining,
		}
		if limit.Remaining <= 0 {
			retryAfter := retryAfterFromUnix(limit.Reset)
			c.log.Warn("github API unavailable due to rate limit", "retry_after", retryAfter)
			return status, &contracts.RateLimitError{Service: "GitHub", RetryAfter: retryAfter}
		}
		c.log.Info("github API availability checked", "authenticated", c.token != "", "remaining", limit.Remaining)
		return status, nil
	case http.StatusUnauthorized:
		return Availability{Authenticated: c.token != "", Remaining: -1}, fmt.Errorf("github token is invalid or unauthorized")
	case http.StatusTooManyRequests, http.StatusForbidden:
		retryAfter := parseRetryAfter(resp)
		c.log.Warn("github API unavailable due to rate limit", "retry_after", retryAfter)
		return Availability{Authenticated: c.token != "", Remaining: 0}, &contracts.RateLimitError{Service: "GitHub", RetryAfter: retryAfter}
	default:
		return Availability{Authenticated: c.token != "", Remaining: -1}, fmt.Errorf("github availability check: unexpected status %d", resp.StatusCode)
	}
}

// CheckToken verifies that the configured token is accepted by GitHub.
func (c *Client) CheckToken(ctx context.Context) error {
	return c.CheckAvailability(ctx)
}

func (c *Client) get(ctx context.Context, url string) (*http.Response, error) {
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

	return resp, nil
}

func (c *Client) checkStatus(resp *http.Response, repo string) error {
	switch resp.StatusCode {
	case http.StatusOK:
		return nil

	case http.StatusNotFound:
		return contracts.ErrRepoNotFound

	case http.StatusUnauthorized:
		return fmt.Errorf("github token is invalid or unauthorized")

	case http.StatusTooManyRequests, http.StatusForbidden:
		// GitHub uses 403 + X-RateLimit-* for primary rate limits and
		// 429 + Retry-After for secondary rate limits.
		retryAfter := parseRetryAfter(resp)
		c.log.Debug("github rate limited", "repo", repo, "retry_after", retryAfter)
		return &contracts.RateLimitError{Service: "GitHub", RetryAfter: retryAfter}

	default:
		return fmt.Errorf("github API unexpected status %d for %s", resp.StatusCode, repo)
	}
}

func (c *Client) closeBody(body io.Closer) {
	if err := body.Close(); err != nil {
		c.log.Warn("close github response body", "err", err)
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

func decodeRateLimit(body io.Reader) (rateLimitResource, error) {
	var rateLimit rateLimitResponse
	if err := json.NewDecoder(body).Decode(&rateLimit); err != nil {
		return rateLimitResource{}, fmt.Errorf("decode github rate limit response: %w", err)
	}
	if rateLimit.Resources.Core.Reset != 0 || rateLimit.Resources.Core.Remaining != 0 {
		return rateLimit.Resources.Core, nil
	}
	return rateLimit.Rate, nil
}

func retryAfterFromUnix(unix int64) time.Duration {
	if unix == 0 {
		return 60 * time.Second
	}
	if d := time.Until(time.Unix(unix, 0)); d > 0 {
		return d
	}
	return 60 * time.Second
}
