package github

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

type rewriteTransport struct {
	target *url.URL
	next   http.RoundTripper
}

func (t rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = t.target.Scheme
	cloned.URL.Host = t.target.Host
	return t.next.RoundTrip(cloned)
}

func newTestClient(t *testing.T, token string, handler http.Handler) (*Client, *atomic.Int32) {
	t.Helper()

	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		handler.ServeHTTP(w, r)
	}))
	t.Cleanup(server.Close)

	targetURL, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}

	client := NewClient(token, nil)
	client.http = server.Client()
	client.http.Transport = rewriteTransport{
		target: targetURL,
		next:   server.Client().Transport,
	}

	return client, &calls
}

func TestClient_GetLatestRelease_Success(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo/releases/latest" {
			t.Fatalf("path = %s, want /repos/owner/repo/releases/latest", r.URL.Path)
		}
		if got := r.Header.Get("Accept"); got != "application/vnd.github+json" {
			t.Fatalf("Accept = %q, want application/vnd.github+json", got)
		}
		if got := r.Header.Get("X-GitHub-Api-Version"); got != "2022-11-28" {
			t.Fatalf("X-GitHub-Api-Version = %q, want 2022-11-28", got)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"tag_name":"v1.2.3","html_url":"https://github.com/owner/repo/releases/tag/v1.2.3","name":"Release 1.2.3"}`)
	}))

	release, err := client.GetLatestRelease(context.Background(), "owner/repo")
	if err != nil {
		t.Fatalf("GetLatestRelease returned error: %v", err)
	}
	if release.TagName != "v1.2.3" {
		t.Fatalf("TagName = %q, want v1.2.3", release.TagName)
	}
	if release.HTMLURL != "https://github.com/owner/repo/releases/tag/v1.2.3" {
		t.Fatalf("HTMLURL = %q, want GitHub release URL", release.HTMLURL)
	}
	if release.Name != "Release 1.2.3" {
		t.Fatalf("Name = %q, want Release 1.2.3", release.Name)
	}
}

func TestClient_GetLatestRelease_WithTokenSendsBearerAuth(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, "test-token", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-token" {
			t.Fatalf("Authorization = %q, want Bearer test-token", got)
		}
		_, _ = fmt.Fprint(w, `{"tag_name":"v1.2.3"}`)
	}))

	if _, err := client.GetLatestRelease(context.Background(), "owner/repo"); err != nil {
		t.Fatalf("GetLatestRelease returned error: %v", err)
	}
}

func TestClient_GetLatestRelease_NotFoundMapsToDomainError(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, "", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	_, err := client.GetLatestRelease(context.Background(), "owner/repo")
	if !errors.Is(err, subscriptions.ErrRepoNotFound) {
		t.Fatalf("error = %v, want ErrRepoNotFound", err)
	}
}

func TestClient_GetLatestRelease_TooManyRequestsUsesRetryAfter(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, "", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Retry-After", "7")
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))

	_, err := client.GetLatestRelease(context.Background(), "owner/repo")
	var rateLimitErr *subscriptions.RateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("error = %v, want RateLimitError", err)
	}
	if rateLimitErr.Service != "GitHub" {
		t.Fatalf("Service = %q, want GitHub", rateLimitErr.Service)
	}
	if rateLimitErr.RetryAfter != 7*time.Second {
		t.Fatalf("RetryAfter = %s, want 7s", rateLimitErr.RetryAfter)
	}
}

func TestClient_GetLatestRelease_ForbiddenRateLimitUsesResetHeader(t *testing.T) {
	t.Parallel()

	resetAt := time.Now().Add(2 * time.Minute)
	client, _ := newTestClient(t, "", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("X-RateLimit-Reset", fmt.Sprint(resetAt.Unix()))
		http.Error(w, "rate limited", http.StatusForbidden)
	}))

	_, err := client.GetLatestRelease(context.Background(), "owner/repo")
	var rateLimitErr *subscriptions.RateLimitError
	if !errors.As(err, &rateLimitErr) {
		t.Fatalf("error = %v, want RateLimitError", err)
	}
	if rateLimitErr.RetryAfter <= 0 {
		t.Fatalf("RetryAfter = %s, want positive duration", rateLimitErr.RetryAfter)
	}
	if rateLimitErr.RetryAfter > 2*time.Minute {
		t.Fatalf("RetryAfter = %s, want at most 2m", rateLimitErr.RetryAfter)
	}
}

func TestClient_GetLatestRelease_InvalidJSONReturnsDecodeError(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, "", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = fmt.Fprint(w, `{"tag_name":`)
	}))

	_, err := client.GetLatestRelease(context.Background(), "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "decode github release response") {
		t.Fatalf("error = %v, want decode github release response", err)
	}
}

func TestClient_GetLatestRelease_UnexpectedStatusIncludesStatusAndRepo(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, "", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}))

	_, err := client.GetLatestRelease(context.Background(), "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "github API unexpected status 500 for owner/repo") {
		t.Fatalf("error = %v, want unexpected status with repo", err)
	}
}

func TestClient_GetLatestRelease_InvalidRepoDoesNotCallHTTP(t *testing.T) {
	t.Parallel()

	client, calls := newTestClient(t, "", http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not be called for invalid repo")
	}))

	_, err := client.GetLatestRelease(context.Background(), "owneronly")
	if err == nil || !strings.Contains(err.Error(), "expected owner/repo") {
		t.Fatalf("error = %v, want invalid repo format", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("HTTP calls = %d, want 0", calls.Load())
	}
}

func TestClient_ValidateRepo_Success(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, "", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Fatalf("method = %s, want GET", r.Method)
		}
		if r.URL.Path != "/repos/owner/repo" {
			t.Fatalf("path = %s, want /repos/owner/repo", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
	}))

	if err := client.ValidateRepo(context.Background(), "owner/repo"); err != nil {
		t.Fatalf("ValidateRepo returned error: %v", err)
	}
}

func TestClient_ValidateRepo_NotFound(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, "", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	}))

	err := client.ValidateRepo(context.Background(), "owner/repo")
	if !errors.Is(err, subscriptions.ErrRepoNotFound) {
		t.Fatalf("error = %v, want ErrRepoNotFound", err)
	}
}

func TestClient_ValidateRepo_UnexpectedStatusIncludesStatusAndRepo(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, "", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusInternalServerError)
	}))

	err := client.ValidateRepo(context.Background(), "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "github API unexpected status 500 for owner/repo") {
		t.Fatalf("error = %v, want unexpected status with repo", err)
	}
}

func TestClient_ValidateRepo_InvalidRepoDoesNotCallHTTP(t *testing.T) {
	t.Parallel()

	client, calls := newTestClient(t, "", http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		t.Fatal("handler should not be called for invalid repo")
	}))

	err := client.ValidateRepo(context.Background(), "owneronly")
	if err == nil || !strings.Contains(err.Error(), "expected owner/repo") {
		t.Fatalf("error = %v, want invalid repo format", err)
	}
	if calls.Load() != 0 {
		t.Fatalf("HTTP calls = %d, want 0", calls.Load())
	}
}

func TestClient_GetLatestRelease_ContextCancellation(t *testing.T) {
	t.Parallel()

	client, _ := newTestClient(t, "", http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := client.GetLatestRelease(ctx, "owner/repo")
	if err == nil || !strings.Contains(err.Error(), "github request") {
		t.Fatalf("error = %v, want github request context error", err)
	}
}

func TestParseRetryAfter(t *testing.T) {
	t.Parallel()

	futureReset := time.Now().Add(2 * time.Minute).Unix()
	pastReset := time.Now().Add(-2 * time.Minute).Unix()

	tests := []struct {
		name      string
		headers   map[string]string
		want      time.Duration
		wantRange bool
		wantMin   time.Duration
		wantMax   time.Duration
	}{
		{
			name:    "uses positive retry after seconds",
			headers: map[string]string{"Retry-After": "7"},
			want:    7 * time.Second,
		},
		{
			name:      "uses reset header when retry after is invalid",
			headers:   map[string]string{"Retry-After": "soon", "X-RateLimit-Reset": fmt.Sprint(futureReset)},
			wantRange: true,
			wantMin:   0,
			wantMax:   2 * time.Minute,
		},
		{
			name:      "uses reset header when retry after is zero",
			headers:   map[string]string{"Retry-After": "0", "X-RateLimit-Reset": fmt.Sprint(futureReset)},
			wantRange: true,
			wantMin:   0,
			wantMax:   2 * time.Minute,
		},
		{
			name:    "falls back when reset header is expired",
			headers: map[string]string{"X-RateLimit-Reset": fmt.Sprint(pastReset)},
			want:    60 * time.Second,
		},
		{
			name:    "falls back when headers are absent",
			headers: map[string]string{},
			want:    60 * time.Second,
		},
		{
			name:    "falls back when reset header is invalid",
			headers: map[string]string{"X-RateLimit-Reset": "later"},
			want:    60 * time.Second,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			resp := &http.Response{Header: make(http.Header)}
			for key, value := range tt.headers {
				resp.Header.Set(key, value)
			}

			got := parseRetryAfter(resp)
			if tt.wantRange {
				if got <= tt.wantMin || got > tt.wantMax {
					t.Fatalf("parseRetryAfter() = %s, want > %s and <= %s", got, tt.wantMin, tt.wantMax)
				}
				return
			}
			if got != tt.want {
				t.Fatalf("parseRetryAfter() = %s, want %s", got, tt.want)
			}
		})
	}
}
