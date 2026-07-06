//go:build integration

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/commands"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres"
	subscriptionapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app"
)

// abortingDeleter implements SubscriptionDeleter by running a deliberately
// failing query on the same Postgres transaction — simulating a Postgres-level
// error during compensation delete that aborts the transaction.
type abortingDeleter struct {
	pool *pgxpool.Pool
}

func (d *abortingDeleter) DeleteSubscription(ctx context.Context, id string) error {
	exec := postgres.ExecutorFromContext(ctx, d.pool)
	_, err := exec.Exec(ctx, `SELECT 1/0`)
	return err
}

func TestIntegration_SagaCompensation_RecordsFailedAttemptAfterAbortedTx(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	repos := newTestRepos(t, ctx)
	truncateAsyncDeliveryState(t, ctx, repos.pool)

	sagaRepo := NewSagaRepository(repos.pool)

	sagaID := "00000000-0000-0000-0000-000000000001"
	subID := "00000000-0000-0000-0000-000000000002"

	if err := sagaRepo.CreateSaga(ctx, sagaID, subID); err != nil {
		t.Fatalf("create saga: %v", err)
	}

	orch := subscriptionapp.NewSagaOrchestrator(&subscriptionapp.SagaOrchestratorDeps{
		SagaRepo:  sagaRepo,
		SubRepo:   &abortingDeleter{pool: repos.pool},
		TxManager: repos.token,
		Log:       logger.NoopLogger{},
	})

	evt := commands.ConfirmationEmailFailed{SagaID: sagaID, Reason: "test failure"}
	if err := orch.OnConfirmationEmailFailed(ctx, evt); err != nil {
		t.Fatalf("OnConfirmationEmailFailed: %v", err)
	}

	_, step, lastErr, err := sagaRepo.Get(ctx, sagaID)
	if err != nil {
		t.Fatalf("get saga: %v", err)
	}
	if step != subscriptionapp.SagaStepCompensating {
		t.Errorf("step = %q, want %q", step, subscriptionapp.SagaStepCompensating)
	}
	if lastErr == nil || !strings.Contains(*lastErr, "compensation attempt 1 failed") {
		t.Errorf("last_error = %v, want compensation attempt 1 failure message", lastErr)
	}
}

func TestIntegration_SagaCompensation_DeadLettersAfterMaxAttempts(t *testing.T) {
	ctx, cancel := integrationContext(t)
	defer cancel()

	repos := newTestRepos(t, ctx)
	truncateAsyncDeliveryState(t, ctx, repos.pool)

	sagaRepo := NewSagaRepository(repos.pool)

	sagaID := "00000000-0000-0000-0000-000000000003"
	subID := "00000000-0000-0000-0000-000000000004"

	if err := sagaRepo.CreateSaga(ctx, sagaID, subID); err != nil {
		t.Fatalf("create saga: %v", err)
	}

	orch := subscriptionapp.NewSagaOrchestrator(&subscriptionapp.SagaOrchestratorDeps{
		SagaRepo:              sagaRepo,
		SubRepo:               &abortingDeleter{pool: repos.pool},
		TxManager:             repos.token,
		Log:                   logger.NoopLogger{},
		MaxCompensateAttempts: 2,
	})

	// First failure: saga goes to COMPENSATING with attempts=1
	evt := commands.ConfirmationEmailFailed{SagaID: sagaID, Reason: "fail 1"}
	if err := orch.OnConfirmationEmailFailed(ctx, evt); err != nil {
		t.Fatalf("OnConfirmationEmailFailed (1): %v", err)
	}

	// Second failure: attempts=2 >= max=2 → dead-letter to COMPENSATION_FAILED
	evt2 := commands.ConfirmationEmailFailed{SagaID: sagaID, Reason: "fail 2"}
	if err := orch.OnConfirmationEmailFailed(ctx, evt2); err != nil {
		t.Fatalf("OnConfirmationEmailFailed (2): %v", err)
	}

	_, step, lastErr, err := sagaRepo.Get(ctx, sagaID)
	if err != nil {
		t.Fatalf("get saga: %v", err)
	}
	if step != subscriptionapp.SagaStepCompensationFailed {
		t.Errorf("step = %q, want %q", step, subscriptionapp.SagaStepCompensationFailed)
	}
	if lastErr == nil || !strings.Contains(*lastErr, "compensation dead-lettered") {
		t.Errorf("last_error = %v, want dead-letter message", lastErr)
	}
}
