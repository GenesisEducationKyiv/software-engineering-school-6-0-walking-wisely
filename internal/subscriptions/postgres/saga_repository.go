package postgres

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	platformpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres"
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
		 VALUES ($1, $2, 'AWAITING_EMAIL')`,
		sagaUUID,
		subUUID,
	); err != nil {
		return fmt.Errorf("insert saga: %w", err)
	}
	return nil
}

// SetStep updates the step (and optionally last_error) for the given saga.
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

// Get returns (subscriptionID, step, lastError) for the given sagaID.
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
