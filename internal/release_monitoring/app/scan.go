package releasemonitoringapp

import (
	"context"
	"errors"
	"fmt"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	releasemonitoringdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain"
	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

type ReleaseScanRepo interface {
	ListDistinctConfirmedRepos(ctx context.Context) ([]string, error)
	ListConfirmedSubscribersForRepo(ctx context.Context, repo string) ([]releasemonitoringdomain.Subscriber, error)
	UpdateLastSeenTag(ctx context.Context, repo, tag string) error
}

type ReleaseClient interface {
	GetLatestRelease(ctx context.Context, repo string) (*releasemonitoringdomain.Release, error)
}

type ScannerService struct {
	repo      ReleaseScanRepo
	github    ReleaseClient
	publisher *events.Bus
	log       logger.Logger
}

type ScannerDeps struct {
	Repo      ReleaseScanRepo
	GitHub    ReleaseClient
	Publisher *events.Bus
	Log       logger.Logger
}

func NewScannerService(deps ScannerDeps) *ScannerService {
	log := deps.Log
	if log == nil {
		log = logger.NoopLogger{}
	}
	return &ScannerService{
		repo:      deps.Repo,
		github:    deps.GitHub,
		publisher: deps.Publisher,
		log:       log,
	}
}

type ScanSummary struct {
	ReposTotal    int
	ReposOK       int
	ReposFailed   int
	Notifications int
}

func (s *ScannerService) Scan(ctx context.Context) {
	startRepos, err := s.repo.ListDistinctConfirmedRepos(ctx)
	if err != nil {
		s.log.Error("scanner: list repos failed", "err", err)
		return
	}
	if len(startRepos) == 0 {
		s.log.Info("scanner: scan complete",
			"repos_total", 0,
			"repos_checked", 0,
			"repos_failed", 0,
			"notifications_enqueued", 0)
		return
	}

	summary := ScanSummary{ReposTotal: len(startRepos)}
	s.log.Info("scanner: scanning repos", "count", len(startRepos))

	for _, repo := range startRepos {
		if ctx.Err() != nil {
			break
		}
		notified, err := s.scanRepo(ctx, repo)
		if err != nil {
			summary.ReposFailed++
			continue
		}
		summary.ReposOK++
		summary.Notifications += notified
	}

	s.log.Info("scanner: scan complete",
		"repos_total", summary.ReposTotal,
		"repos_checked", summary.ReposOK,
		"repos_failed", summary.ReposFailed,
		"notifications_enqueued", summary.Notifications)
}

func (s *ScannerService) scanRepo(ctx context.Context, repo string) (int, error) {
	release, err := s.github.GetLatestRelease(ctx, repo)
	if err != nil {
		var rle *subscriptionsdomain.RateLimitError
		if ok := errors.As(err, &rle); ok {
			s.log.Warn("scanner: github rate limited, skipping repo",
				"repo", repo, "retry_after", rle.RetryAfter)
		} else {
			s.log.Error("scanner: get latest release failed", "repo", repo, "err", err)
		}
		return 0, err
	}
	if release == nil {
		s.log.Error("scanner: release client returned nil release", "repo", repo)
		return 0, fmt.Errorf("nil release for %s", repo)
	}

	subscribers, err := s.repo.ListConfirmedSubscribersForRepo(ctx, repo)
	if err != nil {
		s.log.Error("scanner: list subscribers failed", "repo", repo, "err", err)
		return 0, err
	}

	pending := make([]releasemonitoringdomain.Subscriber, 0, len(subscribers))
	for _, subscriber := range subscribers {
		if subscriber.LastSeenTag != nil && *subscriber.LastSeenTag == release.TagName {
			continue
		}
		pending = append(pending, subscriber)
	}

	if len(pending) == 0 {
		return 0, nil
	}

	if err := s.publisher.Publish(ctx, releasemonitoringdomain.ReleaseDetected{
		Repo:        repo,
		Release:     *release,
		Subscribers: pending,
	}); err != nil {
		s.log.Error("scanner: publish release detected failed", "repo", repo, "err", err)
		return 0, err
	}

	if err := s.repo.UpdateLastSeenTag(ctx, repo, release.TagName); err != nil {
		s.log.Error("scanner: update last seen tag failed", "repo", repo, "err", err)
		return 0, err
	}

	return len(pending), nil
}
