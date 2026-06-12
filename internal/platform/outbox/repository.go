package outbox

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	platformpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres"
)

const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusDelivered  = "delivered"
	StatusFailed     = "failed"
)

type Record struct {
	ID             string
	EventType      string
	AggregateType  string
	AggregateID    string
	PayloadJSON    []byte
	OccurredAt     time.Time
	AvailableAt    time.Time
	Status         string
	AttemptCount   int
	LastError      *string
	IdempotencyKey string
}

type MetricsSnapshot struct {
	PendingCount     int64
	OldestPendingAge float64
	RetryCount       int64
	FailedCount      int64
}

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) Append(ctx context.Context, event events.DurableEvent) error {
	eventID, err := uuid.Parse(event.EventID())
	if err != nil {
		return fmt.Errorf("parse event id %q: %w", event.EventID(), err)
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event payload: %w", err)
	}

	exec := platformpostgres.ExecutorFromContext(ctx, r.db)
	if _, err := exec.Exec(
		ctx,
		`INSERT INTO outbox_events
		 (id, event_type, aggregate_type, aggregate_id, payload_json, occurred_at, available_at, status, attempt_count, idempotency_key)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 0, $9)
		 ON CONFLICT (idempotency_key) DO NOTHING`,
		eventID,
		event.EventName(),
		event.AggregateType(),
		event.AggregateID(),
		payload,
		event.OccurredAt(),
		event.OccurredAt(),
		StatusPending,
		event.IdempotencyKey(),
	); err != nil {
		return fmt.Errorf("insert outbox event: %w", err)
	}

	return nil
}

func (r *Repository) ClaimPending(ctx context.Context, workerID string, batchSize int) ([]Record, error) {
	rows, err := r.db.Query(
		ctx,
		`WITH locked AS (
			SELECT id
			FROM outbox_events
			WHERE status IN ('pending', 'processing')
			  AND available_at <= NOW()
			  AND (status <> 'processing' OR locked_at < NOW() - INTERVAL '5 minutes')
			ORDER BY occurred_at
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		), claimed AS (
			UPDATE outbox_events o
			SET status = 'processing',
			    locked_at = NOW(),
			    locked_by = $2,
			    updated_at = NOW()
			FROM locked
			WHERE o.id = locked.id
			RETURNING o.id, o.event_type, o.aggregate_type, o.aggregate_id, o.payload_json,
			          o.occurred_at, o.available_at, o.status, o.attempt_count, o.last_error,
			          o.idempotency_key
		)
		SELECT id::text, event_type, aggregate_type, aggregate_id, payload_json,
		       occurred_at, available_at, status, attempt_count, last_error, idempotency_key
		FROM claimed`,
		batchSize,
		workerID,
	)
	if err != nil {
		return nil, fmt.Errorf("claim outbox events: %w", err)
	}
	defer rows.Close()

	records := make([]Record, 0, batchSize)
	for rows.Next() {
		var record Record
		if err := rows.Scan(
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
			return nil, fmt.Errorf("scan outbox record: %w", err)
		}
		records = append(records, record)
	}

	return records, rows.Err()
}

func (r *Repository) MarkDelivered(ctx context.Context, id string) error {
	if _, err := r.db.Exec(
		ctx,
		`UPDATE outbox_events
		 SET status = 'delivered', locked_at = NULL, locked_by = NULL, last_error = NULL, updated_at = NOW()
		 WHERE id = $1::uuid`,
		id,
	); err != nil {
		return fmt.Errorf("mark outbox delivered: %w", err)
	}
	return nil
}

func (r *Repository) MarkFailed(ctx context.Context, id string, attemptCount, maxAttempts int, cause error) error {
	status := StatusPending
	if attemptCount >= maxAttempts {
		status = StatusFailed
	}
	backoff := time.Duration(1<<maxInt(attemptCount-1, 0)) * time.Second

	if _, err := r.db.Exec(
		ctx,
		`UPDATE outbox_events
		 SET status = $2,
		     attempt_count = $3,
		     last_error = $4,
		     available_at = $5,
		     locked_at = NULL,
		     locked_by = NULL,
		     updated_at = NOW()
		 WHERE id = $1::uuid`,
		id,
		status,
		attemptCount,
		cause.Error(),
		time.Now().UTC().Add(backoff),
	); err != nil {
		return fmt.Errorf("mark outbox failed: %w", err)
	}
	return nil
}

func (r *Repository) DeleteDeliveredBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	tag, err := r.db.Exec(
		ctx,
		`DELETE FROM outbox_events
		 WHERE status = 'delivered'
		   AND updated_at < $1`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("delete delivered outbox events: %w", err)
	}
	return tag.RowsAffected(), nil
}

func (r *Repository) Metrics(ctx context.Context) (MetricsSnapshot, error) {
	var snapshot MetricsSnapshot
	err := r.db.QueryRow(
		ctx,
		`SELECT
			COUNT(*) FILTER (WHERE status = 'pending') AS pending_count,
			COALESCE(EXTRACT(EPOCH FROM NOW() - MIN(occurred_at)) FILTER (WHERE status = 'pending'), 0),
			COUNT(*) FILTER (WHERE attempt_count > 0 AND status <> 'delivered') AS retry_count,
			COUNT(*) FILTER (WHERE status = 'failed') AS failed_count
		FROM outbox_events`,
	).Scan(&snapshot.PendingCount, &snapshot.OldestPendingAge, &snapshot.RetryCount, &snapshot.FailedCount)
	if err != nil {
		return MetricsSnapshot{}, fmt.Errorf("query outbox metrics: %w", err)
	}
	return snapshot, nil
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
