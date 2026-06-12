//go:build integration

package outbox

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	subscriptionpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/postgres"
)

func TestRepositoryClaimPending(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	repo, pool := newTestRepository(t, ctx)
	truncateOutboxEvents(t, ctx, pool)

	insertOutboxRow(t, ctx, pool, outboxSeed{
		ID:             "00000000-0000-0000-0000-000000000001",
		EventType:      "event.pending.ready",
		AggregateType:  "subscription",
		AggregateID:    "sub-1",
		PayloadJSON:    `{"kind":"ready"}`,
		OccurredAt:     time.Now().UTC().Add(-4 * time.Minute),
		AvailableAt:    time.Now().UTC().Add(-1 * time.Minute),
		Status:         StatusPending,
		IdempotencyKey: "key-ready",
	})
	insertOutboxRow(t, ctx, pool, outboxSeed{
		ID:             "00000000-0000-0000-0000-000000000002",
		EventType:      "event.processing.expired",
		AggregateType:  "subscription",
		AggregateID:    "sub-2",
		PayloadJSON:    `{"kind":"expired"}`,
		OccurredAt:     time.Now().UTC().Add(-3 * time.Minute),
		AvailableAt:    time.Now().UTC().Add(-1 * time.Minute),
		Status:         StatusProcessing,
		LockedAt:       ptrTime(time.Now().UTC().Add(-10 * time.Minute)),
		LockedBy:       ptrString("worker-stale"),
		IdempotencyKey: "key-expired",
	})
	insertOutboxRow(t, ctx, pool, outboxSeed{
		ID:             "00000000-0000-0000-0000-000000000003",
		EventType:      "event.pending.future",
		AggregateType:  "subscription",
		AggregateID:    "sub-3",
		PayloadJSON:    `{"kind":"future"}`,
		OccurredAt:     time.Now().UTC().Add(-2 * time.Minute),
		AvailableAt:    time.Now().UTC().Add(5 * time.Minute),
		Status:         StatusPending,
		IdempotencyKey: "key-future",
	})
	insertOutboxRow(t, ctx, pool, outboxSeed{
		ID:             "00000000-0000-0000-0000-000000000004",
		EventType:      "event.processing.fresh",
		AggregateType:  "subscription",
		AggregateID:    "sub-4",
		PayloadJSON:    `{"kind":"fresh"}`,
		OccurredAt:     time.Now().UTC().Add(-1 * time.Minute),
		AvailableAt:    time.Now().UTC().Add(-1 * time.Minute),
		Status:         StatusProcessing,
		LockedAt:       ptrTime(time.Now().UTC().Add(-1 * time.Minute)),
		LockedBy:       ptrString("worker-fresh"),
		IdempotencyKey: "key-fresh",
	})

	claimed, err := repo.ClaimPending(ctx, "worker-a", 10)
	if err != nil {
		t.Fatalf("ClaimPending returned error: %v", err)
	}

	gotIDs := make([]string, 0, len(claimed))
	for _, record := range claimed {
		gotIDs = append(gotIDs, record.ID)
		if record.Status != StatusProcessing {
			t.Fatalf("claimed status = %q, want processing", record.Status)
		}
	}
	slices.Sort(gotIDs)
	wantIDs := []string{
		"00000000-0000-0000-0000-000000000001",
		"00000000-0000-0000-0000-000000000002",
	}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("claimed ids = %#v, want %#v", gotIDs, wantIDs)
	}
}

func TestRepositoryClaimPendingConcurrentClaimersDoNotOverlap(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	repo, pool := newTestRepository(t, ctx)
	truncateOutboxEvents(t, ctx, pool)

	for i := 0; i < 4; i++ {
		insertOutboxRow(t, ctx, pool, outboxSeed{
			ID:             outboxID(i + 1),
			EventType:      "event.concurrent",
			AggregateType:  "subscription",
			AggregateID:    fmt.Sprintf("sub-%d", i+1),
			PayloadJSON:    `{"kind":"concurrent"}`,
			OccurredAt:     time.Now().UTC().Add(time.Duration(-4+i) * time.Minute),
			AvailableAt:    time.Now().UTC().Add(-1 * time.Minute),
			Status:         StatusPending,
			IdempotencyKey: fmt.Sprintf("key-concurrent-%d", i+1),
		})
	}

	type result struct {
		records []Record
		err     error
	}

	results := make(chan result, 2)
	var start sync.WaitGroup
	start.Add(1)

	for _, workerID := range []string{"worker-a", "worker-b"} {
		go func(workerID string) {
			start.Wait()
			records, err := repo.ClaimPending(ctx, workerID, 2)
			results <- result{records: records, err: err}
		}(workerID)
	}

	start.Done()

	first := <-results
	second := <-results
	if first.err != nil {
		t.Fatalf("first claimer error: %v", first.err)
	}
	if second.err != nil {
		t.Fatalf("second claimer error: %v", second.err)
	}

	seen := map[string]bool{}
	for _, batch := range [][]Record{first.records, second.records} {
		for _, record := range batch {
			if seen[record.ID] {
				t.Fatalf("record %s claimed twice", record.ID)
			}
			seen[record.ID] = true
		}
	}
	if len(seen) != 4 {
		t.Fatalf("unique claimed ids = %d, want 4", len(seen))
	}
}

func TestRepositoryMarkFailedSchedulesRetry(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	repo, pool := newTestRepository(t, ctx)
	truncateOutboxEvents(t, ctx, pool)

	id := "00000000-0000-0000-0000-000000000001"
	insertOutboxRow(t, ctx, pool, outboxSeed{
		ID:             id,
		EventType:      "event.retry",
		AggregateType:  "subscription",
		AggregateID:    "sub-1",
		PayloadJSON:    `{"kind":"retry"}`,
		OccurredAt:     time.Now().UTC().Add(-1 * time.Minute),
		AvailableAt:    time.Now().UTC().Add(-1 * time.Minute),
		Status:         StatusProcessing,
		LockedAt:       ptrTime(time.Now().UTC()),
		LockedBy:       ptrString("worker-a"),
		IdempotencyKey: "key-retry",
	})

	before := time.Now().UTC()
	if err := repo.MarkFailed(ctx, id, 1, 3, errors.New("temporary failure")); err != nil {
		t.Fatalf("MarkFailed returned error: %v", err)
	}

	record := loadOutboxRecord(t, ctx, pool, id)
	if record.Status != StatusPending {
		t.Fatalf("status = %q, want pending", record.Status)
	}
	if record.AttemptCount != 1 {
		t.Fatalf("attempt_count = %d, want 1", record.AttemptCount)
	}
	if record.LastError == nil || *record.LastError != "temporary failure" {
		t.Fatalf("last_error = %#v, want temporary failure", record.LastError)
	}
	if !record.AvailableAt.After(before) {
		t.Fatalf("available_at = %s, want after %s", record.AvailableAt, before)
	}
}

func TestRepositoryMarkFailedMovesToFailedAfterMaxAttempts(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	repo, pool := newTestRepository(t, ctx)
	truncateOutboxEvents(t, ctx, pool)

	id := "00000000-0000-0000-0000-000000000001"
	insertOutboxRow(t, ctx, pool, outboxSeed{
		ID:             id,
		EventType:      "event.failed",
		AggregateType:  "subscription",
		AggregateID:    "sub-1",
		PayloadJSON:    `{"kind":"failed"}`,
		OccurredAt:     time.Now().UTC().Add(-1 * time.Minute),
		AvailableAt:    time.Now().UTC().Add(-1 * time.Minute),
		Status:         StatusProcessing,
		LockedAt:       ptrTime(time.Now().UTC()),
		LockedBy:       ptrString("worker-a"),
		IdempotencyKey: "key-failed",
	})

	if err := repo.MarkFailed(ctx, id, 3, 3, errors.New("permanent failure")); err != nil {
		t.Fatalf("MarkFailed returned error: %v", err)
	}

	record := loadOutboxRecord(t, ctx, pool, id)
	if record.Status != StatusFailed {
		t.Fatalf("status = %q, want failed", record.Status)
	}
	if record.AttemptCount != 3 {
		t.Fatalf("attempt_count = %d, want 3", record.AttemptCount)
	}
}

func TestRepositoryDeleteDeliveredBefore(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	repo, pool := newTestRepository(t, ctx)
	truncateOutboxEvents(t, ctx, pool)

	old := time.Now().UTC().Add(-8 * 24 * time.Hour)
	fresh := time.Now().UTC().Add(-6 * 24 * time.Hour)
	cutoff := time.Now().UTC().Add(-7 * 24 * time.Hour)

	insertOutboxRow(t, ctx, pool, outboxSeed{
		ID:             "00000000-0000-0000-0000-000000000001",
		EventType:      "event.delivered.old",
		AggregateType:  "subscription",
		AggregateID:    "sub-1",
		PayloadJSON:    `{"kind":"old-delivered"}`,
		OccurredAt:     old,
		AvailableAt:    old,
		Status:         StatusDelivered,
		IdempotencyKey: "key-old-delivered",
	})
	setOutboxUpdatedAt(t, ctx, pool, "00000000-0000-0000-0000-000000000001", old)

	insertOutboxRow(t, ctx, pool, outboxSeed{
		ID:             "00000000-0000-0000-0000-000000000002",
		EventType:      "event.delivered.fresh",
		AggregateType:  "subscription",
		AggregateID:    "sub-2",
		PayloadJSON:    `{"kind":"fresh-delivered"}`,
		OccurredAt:     fresh,
		AvailableAt:    fresh,
		Status:         StatusDelivered,
		IdempotencyKey: "key-fresh-delivered",
	})
	setOutboxUpdatedAt(t, ctx, pool, "00000000-0000-0000-0000-000000000002", fresh)

	insertOutboxRow(t, ctx, pool, outboxSeed{
		ID:             "00000000-0000-0000-0000-000000000003",
		EventType:      "event.pending.old",
		AggregateType:  "subscription",
		AggregateID:    "sub-3",
		PayloadJSON:    `{"kind":"old-pending"}`,
		OccurredAt:     old,
		AvailableAt:    old,
		Status:         StatusPending,
		IdempotencyKey: "key-old-pending",
	})
	setOutboxUpdatedAt(t, ctx, pool, "00000000-0000-0000-0000-000000000003", old)

	insertOutboxRow(t, ctx, pool, outboxSeed{
		ID:             "00000000-0000-0000-0000-000000000004",
		EventType:      "event.failed.old",
		AggregateType:  "subscription",
		AggregateID:    "sub-4",
		PayloadJSON:    `{"kind":"old-failed"}`,
		OccurredAt:     old,
		AvailableAt:    old,
		Status:         StatusFailed,
		IdempotencyKey: "key-old-failed",
	})
	setOutboxUpdatedAt(t, ctx, pool, "00000000-0000-0000-0000-000000000004", old)

	deleted, err := repo.DeleteDeliveredBefore(ctx, cutoff)
	if err != nil {
		t.Fatalf("DeleteDeliveredBefore returned error: %v", err)
	}
	if deleted != 1 {
		t.Fatalf("deleted rows = %d, want 1", deleted)
	}

	gotIDs := loadOutboxIDs(t, ctx, pool)
	wantIDs := []string{
		"00000000-0000-0000-0000-000000000002",
		"00000000-0000-0000-0000-000000000003",
		"00000000-0000-0000-0000-000000000004",
	}
	if !slices.Equal(gotIDs, wantIDs) {
		t.Fatalf("remaining ids = %#v, want %#v", gotIDs, wantIDs)
	}
}

func newTestRepository(t *testing.T, ctx context.Context) (*Repository, *pgxpool.Pool) {
	t.Helper()

	testcontainers.SkipIfProviderIsNotHealthy(t)

	container, err := tcpostgres.Run(
		ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("app"),
		tcpostgres.WithUsername("app"),
		tcpostgres.WithPassword("secret"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})

	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("build postgres connection string: %v", err)
	}
	if err := subscriptionpostgres.RunMigrations(databaseURL, logger.NoopLogger{}); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	return NewRepository(pool), pool
}

type outboxSeed struct {
	ID             string
	EventType      string
	AggregateType  string
	AggregateID    string
	PayloadJSON    string
	OccurredAt     time.Time
	AvailableAt    time.Time
	Status         string
	AttemptCount   int
	LastError      *string
	LockedAt       *time.Time
	LockedBy       *string
	IdempotencyKey string
}

func insertOutboxRow(t *testing.T, ctx context.Context, pool *pgxpool.Pool, seed outboxSeed) {
	t.Helper()

	if _, err := pool.Exec(
		ctx,
		`INSERT INTO outbox_events
		 (id, event_type, aggregate_type, aggregate_id, payload_json, occurred_at, available_at, status, attempt_count, last_error, locked_at, locked_by, idempotency_key)
		 VALUES ($1::uuid, $2, $3, $4, $5::jsonb, $6, $7, $8, $9, $10, $11, $12, $13)`,
		seed.ID,
		seed.EventType,
		seed.AggregateType,
		seed.AggregateID,
		seed.PayloadJSON,
		seed.OccurredAt,
		seed.AvailableAt,
		seed.Status,
		seed.AttemptCount,
		seed.LastError,
		seed.LockedAt,
		seed.LockedBy,
		seed.IdempotencyKey,
	); err != nil {
		t.Fatalf("insert outbox row: %v", err)
	}
}

func loadOutboxRecord(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string) Record {
	t.Helper()

	var record Record
	if err := pool.QueryRow(
		ctx,
		`SELECT id::text, event_type, aggregate_type, aggregate_id, payload_json, occurred_at, available_at, status, attempt_count, last_error, idempotency_key
		 FROM outbox_events WHERE id=$1::uuid`,
		id,
	).Scan(
		&record.ID,
		&record.EventType,
		&record.AggregateType,
		&record.AggregateID,
		&record.PayloadJSON,
		&record.OccurredAt,
		&record.AvailableAt,
		&record.Status,
		&record.AttemptCount,
		&record.LastError,
		&record.IdempotencyKey,
	); err != nil {
		t.Fatalf("load outbox record: %v", err)
	}
	return record
}

func truncateOutboxEvents(t *testing.T, ctx context.Context, pool *pgxpool.Pool) {
	t.Helper()

	if _, err := pool.Exec(ctx, `TRUNCATE outbox_events`); err != nil {
		t.Fatalf("truncate outbox events: %v", err)
	}
}

func setOutboxUpdatedAt(t *testing.T, ctx context.Context, pool *pgxpool.Pool, id string, updatedAt time.Time) {
	t.Helper()

	if _, err := pool.Exec(ctx, `UPDATE outbox_events SET updated_at=$2 WHERE id=$1::uuid`, id, updatedAt); err != nil {
		t.Fatalf("set outbox updated_at: %v", err)
	}
}

func loadOutboxIDs(t *testing.T, ctx context.Context, pool *pgxpool.Pool) []string {
	t.Helper()

	rows, err := pool.Query(ctx, `SELECT id::text FROM outbox_events ORDER BY id`)
	if err != nil {
		t.Fatalf("load outbox ids: %v", err)
	}
	defer rows.Close()

	ids := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan outbox id: %v", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate outbox ids: %v", err)
	}
	return ids
}

func ptrString(value string) *string {
	return &value
}

func ptrTime(value time.Time) *time.Time {
	return &value
}

func outboxID(n int) string {
	return fmt.Sprintf("00000000-0000-0000-0000-%012d", n)
}
