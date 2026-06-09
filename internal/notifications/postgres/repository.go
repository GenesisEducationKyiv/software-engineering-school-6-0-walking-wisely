package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/mail"
	platformpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres"
)

const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusSent       = "sent"
	StatusFailed     = "failed"
)

type Job struct {
	ID           string
	EventID      string
	To           string
	Subject      string
	HTML         string
	AttemptCount int
}

type Repository struct {
	db *pgxpool.Pool
}

func NewRepository(db *pgxpool.Pool) *Repository {
	return &Repository{db: db}
}

func (r *Repository) RecordConfirmation(
	ctx context.Context,
	handlerName string,
	eventID string,
	subscriptionID string,
	to string,
	subject string,
	html string,
	confirmToken string,
) error {
	return r.recordDelivery(ctx, handlerName, eventID, func(txCtx context.Context) error {
		exec := platformpostgres.ExecutorFromContext(txCtx, r.db)
		if _, err := exec.Exec(
			txCtx,
			`INSERT INTO notification_jobs
			 (job_type, subscription_id, event_id, to_email, subject, html_body, confirm_token)
			 VALUES ('confirmation', $1::uuid, $2::uuid, $3, $4, $5, $6)
			 ON CONFLICT DO NOTHING`,
			subscriptionID,
			eventID,
			to,
			subject,
			html,
			confirmToken,
		); err != nil {
			return fmt.Errorf("insert confirmation job: %w", err)
		}
		return nil
	})
}

func (r *Repository) RecordReleaseNotifications(
	ctx context.Context,
	handlerName string,
	eventID string,
	releaseTag string,
	jobs []ReleaseNotificationJob,
) error {
	return r.recordDelivery(ctx, handlerName, eventID, func(txCtx context.Context) error {
		exec := platformpostgres.ExecutorFromContext(txCtx, r.db)
		for _, job := range jobs {
			if _, err := exec.Exec(
				txCtx,
				`INSERT INTO notification_jobs
				 (job_type, subscription_id, event_id, to_email, subject, html_body, release_tag)
				 VALUES ('release_notification', $1::uuid, $2::uuid, $3, $4, $5, $6)
				 ON CONFLICT DO NOTHING`,
				job.SubscriptionID,
				eventID,
				job.To,
				job.Subject,
				job.HTML,
				releaseTag,
			); err != nil {
				return fmt.Errorf("insert release job: %w", err)
			}
		}
		return nil
	})
}

type ReleaseNotificationJob struct {
	SubscriptionID string
	To             string
	Subject        string
	HTML           string
}

func (r *Repository) recordDelivery(ctx context.Context, handlerName, eventID string, fn func(context.Context) error) error {
	return platformpostgres.WithinTransaction(ctx, r.db, func(txCtx context.Context) error {
		exec := platformpostgres.ExecutorFromContext(txCtx, r.db)

		// ON CONFLICT DO NOTHING avoids putting the transaction into an error
		// state (which a plain INSERT violation would do in Postgres), so we can
		// safely check the row count to detect duplicate deliveries.
		tag, err := exec.Exec(
			txCtx,
			`INSERT INTO event_deliveries (handler_name, event_id)
			 VALUES ($1, $2::uuid)
			 ON CONFLICT DO NOTHING`,
			handlerName,
			eventID,
		)
		if err != nil {
			return fmt.Errorf("insert event delivery: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil // already delivered — idempotent no-op
		}

		if err := fn(txCtx); err != nil {
			return err
		}

		return nil
	})
}

func (r *Repository) ClaimPending(ctx context.Context, workerID string, batchSize int) ([]Job, error) {
	rows, err := r.db.Query(
		ctx,
		`WITH locked AS (
			SELECT id
			FROM notification_jobs
			WHERE status IN ('pending', 'processing')
			  AND available_at <= NOW()
			  AND (status <> 'processing' OR locked_at < NOW() - INTERVAL '5 minutes')
			ORDER BY created_at
			FOR UPDATE SKIP LOCKED
			LIMIT $1
		), claimed AS (
			UPDATE notification_jobs j
			SET status = 'processing',
			    locked_at = NOW(),
			    locked_by = $2,
			    updated_at = NOW()
			FROM locked
			WHERE j.id = locked.id
			RETURNING j.id::text, j.event_id::text, j.to_email, j.subject, j.html_body, j.attempt_count
		)
		SELECT id, event_id, to_email, subject, html_body, attempt_count FROM claimed`,
		batchSize,
		workerID,
	)
	if err != nil {
		return nil, fmt.Errorf("claim notification jobs: %w", err)
	}
	defer rows.Close()

	jobs := make([]Job, 0, batchSize)
	for rows.Next() {
		var job Job
		if err := rows.Scan(&job.ID, &job.EventID, &job.To, &job.Subject, &job.HTML, &job.AttemptCount); err != nil {
			return nil, fmt.Errorf("scan notification job: %w", err)
		}
		jobs = append(jobs, job)
	}
	return jobs, rows.Err()
}

func (r *Repository) MarkSent(ctx context.Context, jobs []Job) error {
	ids := make([]uuid.UUID, 0, len(jobs))
	for _, job := range jobs {
		ids = append(ids, uuid.MustParse(job.ID))
	}
	if _, err := r.db.Exec(
		ctx,
		`UPDATE notification_jobs
		 SET status = 'sent', sent_at = NOW(), locked_at = NULL, locked_by = NULL, last_error = NULL, updated_at = NOW()
		 WHERE id = ANY($1)`,
		ids,
	); err != nil {
		return fmt.Errorf("mark notification jobs sent: %w", err)
	}
	return nil
}

func (r *Repository) MarkFailed(ctx context.Context, jobs []Job, maxAttempts int, cause error) error {
	for _, job := range jobs {
		status := StatusPending
		attempts := job.AttemptCount + 1
		if attempts >= maxAttempts {
			status = StatusFailed
		}
		backoff := time.Duration(1<<maxInt(attempts-1, 0)) * time.Second
		if _, err := r.db.Exec(
			ctx,
			`UPDATE notification_jobs
			 SET status = $2,
			     attempt_count = $3,
			     last_error = $4,
			     available_at = $5,
			     locked_at = NULL,
			     locked_by = NULL,
			     updated_at = NOW()
			 WHERE id = $1::uuid`,
			job.ID,
			status,
			attempts,
			cause.Error(),
			time.Now().UTC().Add(backoff),
		); err != nil {
			return fmt.Errorf("mark notification job failed: %w", err)
		}
	}
	return nil
}

func (j *Job) Message() mail.Message {
	return mail.Message{
		To:      j.To,
		Subject: j.Subject,
		HTML:    j.HTML,
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
