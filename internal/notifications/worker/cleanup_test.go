package worker

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCleanupRepository struct {
	cutoff time.Time
	err    error
}

func (r *fakeCleanupRepository) DeleteSentBefore(_ context.Context, cutoff time.Time) (int64, error) {
	r.cutoff = cutoff
	return 3, r.err
}

func TestRunCleanupUsesRetentionCutoff(t *testing.T) {
	repo := &fakeCleanupRepository{}
	retention := 7 * 24 * time.Hour
	before := time.Now().UTC().Add(-retention)

	runCleanup(context.Background(), repo, retention, &recordingSenderLogger{})

	after := time.Now().UTC().Add(-retention)
	if repo.cutoff.Before(before) || repo.cutoff.After(after) {
		t.Fatalf("cutoff = %s, want between %s and %s", repo.cutoff, before, after)
	}
}

func TestRunCleanupLogsDeleteErrors(t *testing.T) {
	repo := &fakeCleanupRepository{err: errors.New("db unavailable")}
	log := &recordingSenderLogger{}

	runCleanup(context.Background(), repo, 7*24*time.Hour, log)

	if len(log.errors) != 1 || log.errors[0].msg != "notification job cleanup failed" {
		t.Fatalf("errors = %#v, want cleanup failure log", log.errors)
	}
}
