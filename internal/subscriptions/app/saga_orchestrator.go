package subscriptionapp

import (
	"context"
	"fmt"

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
)

// SagaRepository persists the saga state machine.
type SagaRepository interface {
	CreateSaga(ctx context.Context, sagaID, subscriptionID string) error
	SetStep(ctx context.Context, sagaID, step string, lastErr *string) error
	// Get returns (subscriptionID, step, lastError, error).
	Get(ctx context.Context, sagaID string) (subscriptionID, step string, lastErr *string, err error)
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
	sagaRepo  SagaRepository
	subRepo   SubscriptionDeleter
	publisher events.Publisher
	log       logger.Logger
}

// SagaOrchestratorDeps bundles the dependencies for SagaOrchestrator.
type SagaOrchestratorDeps struct {
	SagaRepo  SagaRepository
	SubRepo   SubscriptionDeleter
	Publisher events.Publisher
	Log       logger.Logger
}

// NewSagaOrchestrator returns a SagaOrchestrator wired with its dependencies.
func NewSagaOrchestrator(deps SagaOrchestratorDeps) *SagaOrchestrator {
	log := deps.Log
	if log == nil {
		log = logger.NoopLogger{}
	}
	return &SagaOrchestrator{
		sagaRepo:  deps.SagaRepo,
		subRepo:   deps.SubRepo,
		publisher: deps.Publisher,
		log:       log,
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
// Idempotent — duplicate events for an already-terminal saga are no-ops.
func (o *SagaOrchestrator) OnConfirmationEmailSent(ctx context.Context, event events.Event) error {
	evt, ok := event.(commands.ConfirmationEmailSent)
	if !ok {
		return fmt.Errorf("unexpected event type %T", event)
	}
	_, step, _, err := o.sagaRepo.Get(ctx, evt.SagaID)
	if err != nil {
		return fmt.Errorf("get saga: %w", err)
	}
	if isTerminal(step) {
		o.log.Info("saga: already terminal, skipping sent reply", "saga_id", evt.SagaID, "step", step)
		return nil
	}
	if err := o.sagaRepo.SetStep(ctx, evt.SagaID, SagaStepCompleted, nil); err != nil {
		return fmt.Errorf("set saga completed: %w", err)
	}
	o.log.Info("saga: completed", "saga_id", evt.SagaID)
	return nil
}

// OnConfirmationEmailFailed handles the failure reply: compensates by deleting the
// subscription. Idempotent — duplicate events re-run compensation safely.
func (o *SagaOrchestrator) OnConfirmationEmailFailed(ctx context.Context, event events.Event) error {
	evt, ok := event.(commands.ConfirmationEmailFailed)
	if !ok {
		return fmt.Errorf("unexpected event type %T", event)
	}
	subscriptionID, step, _, err := o.sagaRepo.Get(ctx, evt.SagaID)
	if err != nil {
		return fmt.Errorf("get saga: %w", err)
	}
	if step == SagaStepCompleted {
		// First terminal reply wins — email succeeded, don't compensate.
		o.log.Info("saga: already completed, ignoring failure reply", "saga_id", evt.SagaID)
		return nil
	}
	if step == SagaStepCompensated {
		o.log.Info("saga: already compensated", "saga_id", evt.SagaID)
		return nil
	}

	// Transition to COMPENSATING (idempotent if already COMPENSATING).
	if step != SagaStepCompensating {
		if err := o.sagaRepo.SetStep(ctx, evt.SagaID, SagaStepCompensating, &evt.Reason); err != nil {
			return fmt.Errorf("set saga compensating: %w", err)
		}
	}

	// C1: delete subscription (idempotent — DELETE where id=X is a no-op if already gone).
	if err := o.subRepo.DeleteSubscription(ctx, subscriptionID); err != nil {
		return fmt.Errorf("compensate: delete subscription: %w", err)
	}

	if err := o.sagaRepo.SetStep(ctx, evt.SagaID, SagaStepCompensated, &evt.Reason); err != nil {
		return fmt.Errorf("set saga compensated: %w", err)
	}
	o.log.Info("saga: compensated", "saga_id", evt.SagaID, "subscription_id", subscriptionID, "reason", evt.Reason)
	return nil
}

// RegisterReplyHandlers attaches the two reply event handlers to the given bus.
func (o *SagaOrchestrator) RegisterReplyHandlers(bus *events.Bus) {
	bus.Subscribe(commands.ConfirmationEmailSent{}.EventName(), o.OnConfirmationEmailSent)
	bus.Subscribe(commands.ConfirmationEmailFailed{}.EventName(), o.OnConfirmationEmailFailed)
}

func isTerminal(step string) bool {
	return step == SagaStepCompleted || step == SagaStepCompensated
}
