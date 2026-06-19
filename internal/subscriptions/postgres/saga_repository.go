package postgres

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres"
	subscriptionapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app"
)

// SagaRepository persists subscription saga state in subscription_sagas.
type SagaRepository struct {
	db *pgxpool.Pool
}

// NewSagaRepository creates a SagaRepository backed by the given pool.
func NewSagaRepository(db *pgxpool.Pool) *SagaRepository {
	return &SagaRepository{db: db}
}

// CreateSaga inserts a new saga row with step=AWAITING_EMAIL.
func (r *SagaRepository) CreateSaga(ctx context.Context, sagaID, subscriptionID string) error {
	sagaUUID, err := uuid.Parse(sagaID)
	if err != nil {
		return fmt.Errorf("parse saga id: %w", err)
	}
	subUUID, err := uuid.Parse(subscriptionID)
	if err != nil {
		return fmt.Errorf("parse subscription id: %w", err)
	}
	exec := platformpostgres.ExecutorFromContext(ctx, r.db)
	if _, err := exec.Exec(
		ctx,
		`INSERT INTO subscription_sagas (saga_id, subscription_id, step)
		 VALUES ($1, $2, $3)`,
		sagaUUID,
		subUUID,
		subscriptionapp.SagaStepAwaitingEmail,
	); err != nil {
		return fmt.Errorf("insert saga: %w", err)
	}
	return nil
}

// SetStep unconditionally updates step + last_error. Must be called within a transaction
// that holds a FOR UPDATE lock on the row (via GetForUpdate).
func (r *SagaRepository) SetStep(ctx context.Context, sagaID, step string, lastErr *string) error {
	exec := platformpostgres.ExecutorFromContext(ctx, r.db)
	if _, err := exec.Exec(
		ctx,
		`UPDATE subscription_sagas
		 SET step = $2, last_error = $3, updated_at = NOW()
		 WHERE saga_id = $1::uuid`,
		sagaID,
		step,
		lastErr,
	); err != nil {
		return fmt.Errorf("update saga step: %w", err)
	}
	return nil
}

// SetCompensateOutcome updates step, compensate_attempts and last_error together. Must be
// called within a transaction that holds a FOR UPDATE lock on the row (via GetForUpdate).
func (r *SagaRepository) SetCompensateOutcome(ctx context.Context, sagaID, step string, attempts int, lastErr *string) error {
	exec := platformpostgres.ExecutorFromContext(ctx, r.db)
	if _, err := exec.Exec(
		ctx,
		`UPDATE subscription_sagas
		 SET step = $2, compensate_attempts = $3, last_error = $4, updated_at = NOW()
		 WHERE saga_id = $1::uuid`,
		sagaID,
		step,
		attempts,
		lastErr,
	); err != nil {
		return fmt.Errorf("update saga compensate outcome: %w", err)
	}
	return nil
}

// Get returns (subscriptionID, step, lastError) for the given sagaID (read-only).
func (r *SagaRepository) Get(ctx context.Context, sagaID string) (subscriptionID, step string, lastErr *string, err error) {
	exec := platformpostgres.ExecutorFromContext(ctx, r.db)
	err = exec.QueryRow(
		ctx,
		`SELECT subscription_id::text, step, last_error
		 FROM subscription_sagas
		 WHERE saga_id = $1::uuid`,
		sagaID,
	).Scan(&subscriptionID, &step, &lastErr)
	if err != nil {
		return "", "", nil, fmt.Errorf("get saga %s: %w", sagaID, err)
	}
	return subscriptionID, step, lastErr, nil
}

// GetForUpdate locks the saga row (SELECT ... FOR UPDATE) and returns its current
// state. Must be called inside an active transaction — use within WithinTransaction.
func (r *SagaRepository) GetForUpdate(ctx context.Context, sagaID string) (subscriptionapp.SagaState, error) {
	exec := platformpostgres.ExecutorFromContext(ctx, r.db)
	var state subscriptionapp.SagaState
	err := exec.QueryRow(
		ctx,
		`SELECT subscription_id::text, step, compensate_attempts, last_error
		 FROM subscription_sagas
		 WHERE saga_id = $1::uuid
		 FOR UPDATE`,
		sagaID,
	).Scan(&state.SubscriptionID, &state.Step, &state.CompensateAttempts, &state.LastError)
	if err != nil {
		return subscriptionapp.SagaState{}, fmt.Errorf("get saga for update %s: %w", sagaID, err)
	}
	return state, nil
}

// StuckSagas returns sagas in non-terminal steps whose updated_at is older than olderThan.
func (r *SagaRepository) StuckSagas(ctx context.Context, olderThan time.Duration) ([]subscriptionapp.SagaRow, error) {
	rows, err := r.db.Query(
		ctx,
		`SELECT saga_id::text, subscription_id::text, step
		 FROM subscription_sagas
		 WHERE step IN ('AWAITING_EMAIL', 'COMPENSATING')
		   AND updated_at < NOW() - make_interval(secs => $1)`,
		int64(olderThan.Seconds()),
	)
	if err != nil {
		return nil, fmt.Errorf("query stuck sagas: %w", err)
	}
	defer rows.Close()

	var result []subscriptionapp.SagaRow
	for rows.Next() {
		var s subscriptionapp.SagaRow
		if err := rows.Scan(&s.SagaID, &s.SubscriptionID, &s.Step); err != nil {
			return nil, fmt.Errorf("scan saga row: %w", err)
		}
		result = append(result, s)
	}
	return result, rows.Err()
}
