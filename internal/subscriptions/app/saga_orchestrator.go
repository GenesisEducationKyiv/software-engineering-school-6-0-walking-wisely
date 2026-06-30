package subscriptionapp

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/commands"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

// Saga step constants — the durable state machine steps persisted in subscription_sagas.
const (
	SagaStepAwaitingEmail = "AWAITING_EMAIL"
	SagaStepCompleted     = "COMPLETED"
	SagaStepCompensating  = "COMPENSATING"
	SagaStepCompensated   = "COMPENSATED"
	// SagaStepCompensationFailed is the dead-letter terminal step: compensation
	// exhausted its retry budget and stopped re-driving. Requires manual intervention.
	SagaStepCompensationFailed = "COMPENSATION_FAILED"
)

// DefaultMaxCompensateAttempts bounds how many times the sweeper re-drives a failing
// compensation before dead-lettering the saga (COMPENSATION_FAILED).
const DefaultMaxCompensateAttempts = 5

// SagaRow is a saga record returned by queries (e.g. the sweeper scan).
type SagaRow struct {
	SagaID         string
	SubscriptionID string
	Step           string
}

// SagaState is the locked saga row read inside a compensation transaction.
type SagaState struct {
	SubscriptionID     string
	Step               string
	CompensateAttempts int
	LastError          *string
}

// SagaRepository persists the saga state machine.
type SagaRepository interface {
	CreateSaga(ctx context.Context, sagaID, subscriptionID string) error
	// SetStep unconditionally updates step. Call only within a tx that holds a FOR UPDATE lock.
	SetStep(ctx context.Context, sagaID, step string, lastErr *string) error
	// SetCompensateOutcome updates step, compensate_attempts and last_error together.
	// Used to persist a failed compensation attempt (retry) or dead-letter it. Call only
	// within a tx that holds a FOR UPDATE lock.
	SetCompensateOutcome(ctx context.Context, sagaID, step string, attempts int, lastErr *string) error
	// Get returns (subscriptionID, step, lastError, error) — read-only.
	Get(ctx context.Context, sagaID string) (subscriptionID, step string, lastErr *string, err error)
	// GetForUpdate locks the row (SELECT ... FOR UPDATE) — must be inside a transaction.
	GetForUpdate(ctx context.Context, sagaID string) (SagaState, error)
	// StuckSagas returns sagas in non-terminal steps older than olderThan.
	StuckSagas(ctx context.Context, olderThan time.Duration) ([]SagaRow, error)
}

// SubscriptionDeleter deletes a subscription by ID (compensation).
type SubscriptionDeleter interface {
	DeleteSubscription(ctx context.Context, id string) error
}

// SagaOrchestrator drives the subscribe confirmation saga:
//
//	T1 (create subscription) + T2 (dispatch confirmation email)
//	Failure of T2 triggers C1 (delete subscription).
type SagaOrchestrator struct {
	sagaRepo              SagaRepository
	subRepo               SubscriptionDeleter
	txManager             TransactionManager
	publisher             events.Publisher
	log                   logger.Logger
	maxCompensateAttempts int
}

// SagaOrchestratorDeps bundles the dependencies for SagaOrchestrator.
type SagaOrchestratorDeps struct {
	SagaRepo  SagaRepository
	SubRepo   SubscriptionDeleter
	TxManager TransactionManager
	Publisher events.Publisher
	Log       logger.Logger
	// MaxCompensateAttempts bounds compensation retries before dead-lettering.
	// Zero falls back to DefaultMaxCompensateAttempts.
	MaxCompensateAttempts int
}

// NewSagaOrchestrator returns a SagaOrchestrator wired with its dependencies.
func NewSagaOrchestrator(deps *SagaOrchestratorDeps) *SagaOrchestrator {
	log := deps.Log
	if log == nil {
		log = logger.NoopLogger{}
	}
	maxAttempts := deps.MaxCompensateAttempts
	if maxAttempts <= 0 {
		maxAttempts = DefaultMaxCompensateAttempts
	}
	return &SagaOrchestrator{
		sagaRepo:              deps.SagaRepo,
		subRepo:               deps.SubRepo,
		txManager:             deps.TxManager,
		publisher:             deps.Publisher,
		log:                   log,
		maxCompensateAttempts: maxAttempts,
	}
}

// EnqueueWithinTx must be called from within an active DB transaction (txCtx).
// It creates the saga row (step=AWAITING_EMAIL) and appends the SendConfirmationEmail
// command to the outbox — both atomically in the caller's transaction.
func (o *SagaOrchestrator) EnqueueWithinTx(
	txCtx context.Context,
	subscriptionID, email, repo, confirmToken, unsubToken string,
) error {
	sagaID := uuid.NewString()
	if err := o.sagaRepo.CreateSaga(txCtx, sagaID, subscriptionID); err != nil {
		return fmt.Errorf("create saga: %w", err)
	}
	cmd := commands.NewSendConfirmationEmail(sagaID, subscriptionID, email, repo, confirmToken, unsubToken)
	if err := o.publisher.Publish(txCtx, cmd); err != nil {
		return fmt.Errorf("enqueue send confirmation email: %w", err)
	}
	o.log.Info("saga: started", "saga_id", sagaID, "subscription_id", subscriptionID)
	return nil
}

// OnConfirmationEmailSent handles the happy-path reply: marks the saga COMPLETED.
// Atomic: SELECT FOR UPDATE + SetStep in one transaction — concurrent/duplicate deliveries
// are serialized and the second one sees the terminal step and skips.
func (o *SagaOrchestrator) OnConfirmationEmailSent(ctx context.Context, event events.Event) error {
	evt, ok := event.(commands.ConfirmationEmailSent)
	if !ok {
		return fmt.Errorf("unexpected event type %T", event)
	}
	return o.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		state, err := o.sagaRepo.GetForUpdate(txCtx, evt.SagaID)
		if err != nil {
			return fmt.Errorf("get saga for update: %w", err)
		}
		if isTerminal(state.Step) {
			o.log.Info("saga: already terminal, skipping sent reply", "saga_id", evt.SagaID, "step", state.Step)
			return nil
		}
		if err := o.sagaRepo.SetStep(txCtx, evt.SagaID, SagaStepCompleted, nil); err != nil {
			return fmt.Errorf("set saga completed: %w", err)
		}
		o.log.Info("saga: completed", "saga_id", evt.SagaID)
		return nil
	})
}

// OnConfirmationEmailFailed handles the failure reply: compensates by deleting the
// subscription. The entire COMPENSATING → delete → COMPENSATED sequence runs in one
// transaction. SELECT FOR UPDATE serializes concurrent/duplicate deliveries.
func (o *SagaOrchestrator) OnConfirmationEmailFailed(ctx context.Context, event events.Event) error {
	evt, ok := event.(commands.ConfirmationEmailFailed)
	if !ok {
		return fmt.Errorf("unexpected event type %T", event)
	}
	return o.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		state, err := o.sagaRepo.GetForUpdate(txCtx, evt.SagaID)
		if err != nil {
			return fmt.Errorf("get saga for update: %w", err)
		}
		return o.doCompensate(txCtx, evt.SagaID, state, evt.Reason)
	})
}

// Sweep re-drives sagas stuck in AWAITING_EMAIL (timed-out) or COMPENSATING (crashed
// mid-compensation). Each saga is processed in its own transaction.
func (o *SagaOrchestrator) Sweep(ctx context.Context, stuckAfter time.Duration) {
	sagas, err := o.sagaRepo.StuckSagas(ctx, stuckAfter)
	if err != nil {
		o.log.Error("sweeper: list stuck sagas", "error", err)
		return
	}
	for _, s := range sagas {
		select {
		case <-ctx.Done():
			return
		default:
		}
		sagaID := s.SagaID
		if err := o.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
			state, err := o.sagaRepo.GetForUpdate(txCtx, sagaID)
			if err != nil {
				return err
			}
			reason := fmt.Sprintf("sweeper re-drive from step %s", state.Step)
			return o.doCompensate(txCtx, sagaID, state, reason)
		}); err != nil {
			o.log.Error("sweeper: compensate saga", "saga_id", sagaID, "error", err)
		}
	}
}

// doCompensate executes the compensation sequence given a FOR-UPDATE-locked saga row.
// AWAITING_EMAIL → COMPENSATING → (delete) → COMPENSATED; already-terminal = no-op.
//
// A failed delete does not roll back the transaction: the incremented attempt counter
// is committed so the sweeper can re-drive with bounded retries. Once attempts reach
// maxCompensateAttempts the saga is dead-lettered (COMPENSATION_FAILED, terminal) so
// the sweeper stops churning on a permanently failing compensation.
func (o *SagaOrchestrator) doCompensate(txCtx context.Context, sagaID string, state SagaState, reason string) error {
	switch state.Step {
	case SagaStepCompleted:
		o.log.Info("saga: already completed, ignoring failure", "saga_id", sagaID)
		return nil
	case SagaStepCompensated:
		o.log.Info("saga: already compensated", "saga_id", sagaID)
		return nil
	case SagaStepCompensationFailed:
		o.log.Info("saga: compensation already dead-lettered, skipping", "saga_id", sagaID)
		return nil
	}

	if state.Step == SagaStepAwaitingEmail {
		if err := o.sagaRepo.SetStep(txCtx, sagaID, SagaStepCompensating, &reason); err != nil {
			return fmt.Errorf("set saga compensating: %w", err)
		}
	}

	if err := o.subRepo.DeleteSubscription(txCtx, state.SubscriptionID); err != nil {
		return o.recordFailedCompensation(txCtx, sagaID, state.CompensateAttempts, err)
	}

	if err := o.sagaRepo.SetStep(txCtx, sagaID, SagaStepCompensated, nil); err != nil {
		return fmt.Errorf("set saga compensated: %w", err)
	}
	o.log.Info("saga: compensated", "saga_id", sagaID, "subscription_id", state.SubscriptionID, "reason", reason)
	return nil
}

// recordFailedCompensation persists a failed delete attempt. It commits (returns nil) so
// the bumped attempt counter survives: either the saga stays COMPENSATING for the next
// sweep, or it is dead-lettered once the retry budget is exhausted.
func (o *SagaOrchestrator) recordFailedCompensation(txCtx context.Context, sagaID string, prevAttempts int, cause error) error {
	attempts := prevAttempts + 1
	if attempts >= o.maxCompensateAttempts {
		msg := fmt.Sprintf("compensation dead-lettered after %d attempts: %v", attempts, cause)
		if err := o.sagaRepo.SetCompensateOutcome(txCtx, sagaID, SagaStepCompensationFailed, attempts, &msg); err != nil {
			return fmt.Errorf("dead-letter saga: %w", err)
		}
		o.log.Error("saga: compensation dead-lettered, manual intervention required",
			"saga_id", sagaID, "attempts", attempts, "error", cause)
		return nil
	}

	msg := fmt.Sprintf("compensation attempt %d failed: %v", attempts, cause)
	if err := o.sagaRepo.SetCompensateOutcome(txCtx, sagaID, SagaStepCompensating, attempts, &msg); err != nil {
		return fmt.Errorf("record compensation attempt: %w", err)
	}
	o.log.Warn("saga: compensation attempt failed, will retry",
		"saga_id", sagaID, "attempts", attempts, "error", cause)
	return nil
}

// RegisterReplyHandlers attaches the two reply event handlers to the given bus.
func (o *SagaOrchestrator) RegisterReplyHandlers(bus *events.Bus) {
	bus.Subscribe(commands.ConfirmationEmailSent{}.EventName(), o.OnConfirmationEmailSent)
	bus.Subscribe(commands.ConfirmationEmailFailed{}.EventName(), o.OnConfirmationEmailFailed)
}

func isTerminal(step string) bool {
	return step == SagaStepCompleted ||
		step == SagaStepCompensated ||
		step == SagaStepCompensationFailed
}
