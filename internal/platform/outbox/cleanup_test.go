package outbox

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeCleanupRepo struct {
	cutoff time.Time
	err    error
}

func (r *fakeCleanupRepo) DeleteDeliveredBefore(_ context.Context, cutoff time.Time) (int64, error) {
	r.cutoff = cutoff
	return 3, r.err
}

func TestRunCleanupUsesRetentionCutoff(t *testing.T) {
	repo := &fakeCleanupRepo{}
	retention := 7 * 24 * time.Hour
	before := time.Now().UTC().Add(-retention)

	runCleanup(context.Background(), repo, retention, &recordingLogger{})

	after := time.Now().UTC().Add(-retention)
	if repo.cutoff.Before(before) || repo.cutoff.After(after) {
		t.Fatalf("cutoff = %s, want between %s and %s", repo.cutoff, before, after)
	}
}

func TestRunCleanupLogsDeleteErrors(t *testing.T) {
	repo := &fakeCleanupRepo{err: errors.New("db unavailable")}
	log := &recordingLogger{}

	runCleanup(context.Background(), repo, 7*24*time.Hour, log)

	if !log.hasMessage("outbox cleanup failed") {
		t.Fatalf("expected cleanup failure log entry, got: %v", log.errors)
	}
}
