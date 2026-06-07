package worker

import (
	"context"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	releasemonitoringapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/app"
	releasemonitoringdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain"
)

type cancelOnListRepo struct {
	cancel context.CancelFunc
	calls  int
	called chan struct{}
}

func (r *cancelOnListRepo) ListDistinctConfirmedRepos(context.Context) ([]string, error) {
	r.calls++
	r.cancel()
	select {
	case <-r.called:
	default:
		close(r.called)
	}
	return nil, nil
}

func (r *cancelOnListRepo) ListConfirmedSubscribersForRepo(context.Context, string) ([]releasemonitoringdomain.Subscriber, error) {
	return nil, nil
}

func (r *cancelOnListRepo) UpdateLastSeenTag(context.Context, string, string) error {
	return nil
}

type noopReleaseClient struct{}

func (noopReleaseClient) GetLatestRelease(context.Context, string) (*releasemonitoringdomain.Release, error) {
	return nil, nil
}

func TestStartScannerRunsUntilContextCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	repo := &cancelOnListRepo{
		cancel: cancel,
		called: make(chan struct{}),
	}
	service := releasemonitoringapp.NewScannerService(releasemonitoringapp.ScannerDeps{
		Repo:   repo,
		GitHub: noopReleaseClient{},
		Log:    logger.NoopLogger{},
	})
	done := make(chan struct{})

	go func() {
		defer close(done)
		StartScanner(ctx, service, time.Millisecond, logger.NoopLogger{})
	}()

	select {
	case <-repo.called:
	case <-time.After(time.Second):
		t.Fatal("scanner did not run a scan")
	}

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("scanner did not stop after context cancellation")
	}

	if repo.calls != 1 {
		t.Fatalf("list repos calls = %d, want 1", repo.calls)
	}
}
