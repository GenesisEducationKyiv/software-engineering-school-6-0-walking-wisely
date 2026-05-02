// Package workers contains the background goroutines that scan GitHub for new releases and dispatch notification emails.
package workers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/clients"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/domain"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/repository"
)

// ScannerDeps bundles the scanner's external dependencies.
type ScannerDeps struct {
	Repo      *repository.SubscriptionRepo
	GitHub    *clients.GitHubClient
	EmailChan chan<- domain.EmailMessage
	BaseURL   string
}

// StartScanner runs the release-scan loop on a fixed ticker until ctx is cancelled.
// Each tick it queries the set of watched repos, fetches the latest release for
// each (via the cached GitHub client), and enqueues notification emails for any
// subscriber whose last_seen_tag differs from the current release.
func StartScanner(ctx context.Context, deps ScannerDeps, interval time.Duration) {
	slog.Info("scanner started", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("scanner stopped")
			return
		case <-ticker.C:
			runScan(ctx, deps)
		}
	}
}

func runScan(ctx context.Context, deps ScannerDeps) {
	repos, err := deps.Repo.ListDistinctConfirmedRepos(ctx)
	if err != nil {
		slog.Error("scanner: list repos failed", "err", err)
		return
	}
	if len(repos) == 0 {
		return
	}

	slog.Info("scanner: scanning repos", "count", len(repos))

	for _, repo := range repos {
		if ctx.Err() != nil {
			return
		}
		scanRepo(ctx, deps, repo)
	}
}

func scanRepo(ctx context.Context, deps ScannerDeps, repo string) {
	release, err := deps.GitHub.GetLatestRelease(ctx, repo)
	if err != nil {
		if rle, ok := domain.AsRateLimitError(err); ok {
			slog.Warn("scanner: github rate limited, skipping repo",
				"repo", repo, "retry_after", rle.RetryAfter)
		} else {
			slog.Error("scanner: get latest release failed", "repo", repo, "err", err)
		}
		return
	}

	subscribers, err := deps.Repo.ListConfirmedSubscribersForRepo(ctx, repo)
	if err != nil {
		slog.Error("scanner: list subscribers failed", "repo", repo, "err", err)
		return
	}

	var notified int
	for i := range subscribers {
		sub := &subscribers[i]
		if sub.LastSeenTag != nil && *sub.LastSeenTag == release.TagName {
			continue // already notified about this release
		}
		msg := buildReleaseEmail(sub, release, deps.BaseURL)
		select {
		case deps.EmailChan <- msg:
			notified++
		default:
			// Channel full - log with subscription_id, not email, to avoid PII in logs.
			slog.Warn("scanner: email channel full, dropping notification",
				"subscription_id", sub.ID, "repo", repo)
		}
	}

	if notified > 0 {
		if err := deps.Repo.UpdateLastSeenTag(ctx, repo, release.TagName); err != nil {
			slog.Error("scanner: update last seen tag failed", "repo", repo, "err", err)
		} else {
			slog.Info("scanner: release notifications enqueued",
				"repo", repo, "tag", release.TagName, "notified", notified)
		}
	}
}

func buildReleaseEmail(sub *domain.Subscription, release *clients.Release, baseURL string) domain.EmailMessage {
	releaseName := release.TagName
	if release.Name != "" {
		releaseName = release.Name
	}
	return domain.EmailMessage{
		To:      sub.Email,
		Subject: fmt.Sprintf("[%s] New release: %s", sub.Repo, release.TagName),
		HTML: fmt.Sprintf(`<p>A new release of <strong>%s</strong> is available.</p>
<p><strong>%s</strong></p>
<p><a href="%s">View release on GitHub</a></p>
<hr>
<p><small><a href="%s/api/unsubscribe/%s">Unsubscribe from %s notifications</a></small></p>`,
			sub.Repo, releaseName, release.HTMLURL,
			baseURL, sub.UnsubscribeToken, sub.Repo),
	}
}
