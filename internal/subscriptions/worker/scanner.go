// Package worker contains background subscription jobs.
package worker

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/releases"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

// ReleaseScanRepo provides the subscription data needed by the release scanner.
type ReleaseScanRepo interface {
	ListDistinctConfirmedRepos(ctx context.Context) ([]string, error)
	ListConfirmedSubscribersForRepo(ctx context.Context, repo string) ([]subscriptions.Subscription, error)
	UpdateLastSeenTag(ctx context.Context, repo, tag string) error
}

// ReleaseClient fetches release information for a repository.
type ReleaseClient interface {
	GetLatestRelease(ctx context.Context, repo string) (*releases.Release, error)
}

// ScannerDeps bundles the scanner's external dependencies.
type ScannerDeps struct {
	Repo      ReleaseScanRepo
	GitHub    ReleaseClient
	EmailChan chan<- mail.Message
	BaseURL   string
	Log       logger.Logger
}

// StartScanner runs the release-scan loop on a fixed ticker until ctx is cancelled.
// Each tick it queries the set of watched repos, fetches the latest release for
// each (via the cached GitHub client), and enqueues notification emails for any
// subscriber whose last_seen_tag differs from the current release.
func StartScanner(ctx context.Context, deps ScannerDeps, interval time.Duration) {
	log := scannerLogger(deps)
	log.Info("scanner started", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("scanner stopped")
			return
		case <-ticker.C:
			runScan(ctx, deps)
		}
	}
}

func runScan(ctx context.Context, deps ScannerDeps) {
	log := scannerLogger(deps)
	start := time.Now()
	repos, err := deps.Repo.ListDistinctConfirmedRepos(ctx)
	if err != nil {
		log.Error("scanner: list repos failed", "err", err)
		return
	}
	if len(repos) == 0 {
		log.Info("scanner: scan complete",
			"repos_total", 0,
			"repos_checked", 0,
			"repos_failed", 0,
			"notifications_enqueued", 0,
			"notifications_dropped", 0,
			"duration_ms", time.Since(start).Milliseconds())
		return
	}

	log.Info("scanner: scanning repos", "count", len(repos))
	summary := scanSummary{reposTotal: len(repos)}

	for _, repo := range repos {
		if ctx.Err() != nil {
			break
		}
		result := scanRepo(ctx, deps, repo)
		if result.checked {
			summary.reposChecked++
		}
		if result.failed {
			summary.reposFailed++
		}
		summary.notificationsEnqueued += result.notificationsEnqueued
		summary.notificationsDropped += result.notificationsDropped
	}

	log.Info("scanner: scan complete",
		"repos_total", summary.reposTotal,
		"repos_checked", summary.reposChecked,
		"repos_failed", summary.reposFailed,
		"notifications_enqueued", summary.notificationsEnqueued,
		"notifications_dropped", summary.notificationsDropped,
		"duration_ms", time.Since(start).Milliseconds())
}

type scanSummary struct {
	reposTotal            int
	reposChecked          int
	reposFailed           int
	notificationsEnqueued int
	notificationsDropped  int
}

type scanRepoResult struct {
	checked               bool
	failed                bool
	notificationsEnqueued int
	notificationsDropped  int
}

func scanRepo(ctx context.Context, deps ScannerDeps, repo string) scanRepoResult {
	log := scannerLogger(deps)
	result := scanRepoResult{checked: true}
	release, err := deps.GitHub.GetLatestRelease(ctx, repo)
	if err != nil {
		result.failed = true
		var rle *subscriptions.RateLimitError
		if ok := errors.As(err, &rle); ok {
			log.Warn("scanner: github rate limited, skipping repo",
				"repo", repo, "retry_after", rle.RetryAfter)
		} else {
			log.Error("scanner: get latest release failed", "repo", repo, "err", err)
		}
		return result
	}
	if release == nil {
		log.Error("scanner: release client returned nil release", "repo", repo)
		result.failed = true
		return result
	}

	subscribers, err := deps.Repo.ListConfirmedSubscribersForRepo(ctx, repo)
	if err != nil {
		log.Error("scanner: list subscribers failed", "repo", repo, "err", err)
		result.failed = true
		return result
	}

	var notified int
	var dropped int
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
			dropped++
			// Channel full - log with subscription_id, not email, to avoid PII in logs.
			log.Warn("scanner: email channel full, dropping notification",
				"subscription_id", sub.ID, "repo", repo)
		}
	}
	result.notificationsEnqueued = notified
	result.notificationsDropped = dropped

	if notified > 0 {
		if err := deps.Repo.UpdateLastSeenTag(ctx, repo, release.TagName); err != nil {
			log.Error("scanner: update last seen tag failed", "repo", repo, "err", err)
			result.failed = true
		} else {
			log.Info("scanner: release notifications enqueued",
				"repo", repo, "tag", release.TagName, "notified", notified)
		}
	}
	return result
}

func scannerLogger(deps ScannerDeps) logger.Logger {
	if deps.Log == nil {
		return logger.NoopLogger{}
	}
	return deps.Log
}

func buildReleaseEmail(sub *subscriptions.Subscription, release *releases.Release, baseURL string) mail.Message {
	releaseName := release.TagName
	if release.Name != "" {
		releaseName = release.Name
	}
	return mail.Message{
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
