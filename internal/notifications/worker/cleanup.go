package worker

import (
	"context"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

type CleanupRepository interface {
	DeleteSentBefore(ctx context.Context, cutoff time.Time) (int64, error)
}

func StartCleanup(
	ctx context.Context,
	repo CleanupRepository,
	interval time.Duration,
	retention time.Duration,
	log logger.Logger,
) {
	if log == nil {
		log = logger.NoopLogger{}
	}
	if interval <= 0 {
		interval = 30 * time.Minute
	}
	if retention <= 0 {
		retention = 7 * 24 * time.Hour
	}

	log.Info("notification job cleanup started", "interval", interval, "retention", retention)
	runCleanup(ctx, repo, retention, log)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("notification job cleanup stopped")
			return
		case <-ticker.C:
			runCleanup(ctx, repo, retention, log)
		}
	}
}

func runCleanup(ctx context.Context, repo CleanupRepository, retention time.Duration, log logger.Logger) {
	deleted, err := repo.DeleteSentBefore(ctx, time.Now().UTC().Add(-retention))
	if err != nil {
		log.Error("notification job cleanup failed", "err", err)
		return
	}
	if deleted > 0 {
		log.Info("notification job cleanup deleted sent jobs", "count", deleted)
	}
}
