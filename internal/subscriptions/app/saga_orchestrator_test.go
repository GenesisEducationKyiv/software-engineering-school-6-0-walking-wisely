package subscriptionapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/commands"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
)

// ---- fakes ----

type fakeSagaState struct {
	subscriptionID string
	step           string
	attempts       int
	lastErr        *string
}

type fakeSagaRepo struct {
	sagas      map[string]*fakeSagaState
	setSteps   []string // ordered log of step values written
	stuckSagas []SagaRow
}

func newFakeSagaRepo() *fakeSagaRepo {
	return &fakeSagaRepo{sagas: make(map[string]*fakeSagaState)}
}

func (r *fakeSagaRepo) seed(sagaID, subscriptionID, step string) {
	r.sagas[sagaID] = &fakeSagaState{subscriptionID: subscriptionID, step: step}
}

func (r *fakeSagaRepo) CreateSaga(ctx context.Context, sagaID, subscriptionID string) error {
	r.sagas[sagaID] = &fakeSagaState{subscriptionID: subscriptionID, step: SagaStepAwaitingEmail}
	return nil
}

func (r *fakeSagaRepo) SetStep(_ context.Context, sagaID, step string, lastErr *string) error {
	s, ok := r.sagas[sagaID]
	if !ok {
		return errors.New("saga not found")
	}
	s.step = step
	s.lastErr = lastErr
	r.setSteps = append(r.setSteps, step)
	return nil
}

func (r *fakeSagaRepo) SetCompensateOutcome(_ context.Context, sagaID, step string, attempts int, lastErr *string) error {
	s, ok := r.sagas[sagaID]
	if !ok {
		return errors.New("saga not found")
	}
	s.step = step
	s.attempts = attempts
	s.lastErr = lastErr
	r.setSteps = append(r.setSteps, step)
	return nil
}

func (r *fakeSagaRepo) Get(_ context.Context, sagaID string) (subscriptionID, step string, lastErr *string, err error) {
	s, ok := r.sagas[sagaID]
	if !ok {
		return "", "", nil, errors.New("saga not found")
	}
	return s.subscriptionID, s.step, s.lastErr, nil
}

func (r *fakeSagaRepo) GetForUpdate(_ context.Context, sagaID string) (SagaState, error) {
	s, ok := r.sagas[sagaID]
	if !ok {
		return SagaState{}, errors.New("saga not found")
	}
	return SagaState{
		SubscriptionID:     s.subscriptionID,
		Step:               s.step,
		CompensateAttempts: s.attempts,
		LastError:          s.lastErr,
	}, nil
}

func (r *fakeSagaRepo) StuckSagas(_ context.Context, _ time.Duration) ([]SagaRow, error) {
	return r.stuckSagas, nil
}

type fakeSubDeleter struct {
	deleted []string
	err     error
}

func (f *fakeSubDeleter) DeleteSubscription(_ context.Context, id string) error {
	if f.err != nil {
		return f.err
	}
	f.deleted = append(f.deleted, id)
	return nil
}

func newOrchestrator(repo *fakeSagaRepo, deleter *fakeSubDeleter) *SagaOrchestrator {
	return NewSagaOrchestrator(&SagaOrchestratorDeps{
		SagaRepo:  repo,
		SubRepo:   deleter,
		TxManager: fakeTxManager{},
	})
}

// ---- helpers ----

func sentEvent(sagaID string) events.Event {
	return commands.ConfirmationEmailSent{SagaID: sagaID}
}

func failedEvent(sagaID, reason string) events.Event {
	return commands.ConfirmationEmailFailed{SagaID: sagaID, Reason: reason}
}

// ---- OnConfirmationEmailSent ----

func TestOnConfirmationEmailSent_HappyPath(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-1", "sub-1", SagaStepAwaitingEmail)
	del := &fakeSubDeleter{}
	orch := newOrchestrator(repo, del)

	if err := orch.OnConfirmationEmailSent(context.Background(), sentEvent("saga-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.sagas["saga-1"].step != SagaStepCompleted {
		t.Errorf("step = %q, want COMPLETED", repo.sagas["saga-1"].step)
	}
	if len(del.deleted) != 0 {
		t.Errorf("unexpected subscription deletions: %v", del.deleted)
	}
}

func TestOnConfirmationEmailSent_AlreadyCompleted_NoOp(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-1", "sub-1", SagaStepCompleted)
	del := &fakeSubDeleter{}
	orch := newOrchestrator(repo, del)

	if err := orch.OnConfirmationEmailSent(context.Background(), sentEvent("saga-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.setSteps) != 0 {
		t.Errorf("SetStep called %d times, want 0", len(repo.setSteps))
	}
}

func TestOnConfirmationEmailSent_AlreadyCompensated_NoOp(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-1", "sub-1", SagaStepCompensated)
	del := &fakeSubDeleter{}
	orch := newOrchestrator(repo, del)

	if err := orch.OnConfirmationEmailSent(context.Background(), sentEvent("saga-1")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(repo.setSteps) != 0 {
		t.Errorf("SetStep called %d times, want 0", len(repo.setSteps))
	}
}

// ---- OnConfirmationEmailFailed ----

func TestOnConfirmationEmailFailed_HappyPath(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-1", "sub-1", SagaStepAwaitingEmail)
	del := &fakeSubDeleter{}
	orch := newOrchestrator(repo, del)

	if err := orch.OnConfirmationEmailFailed(context.Background(), failedEvent("saga-1", "provider reject")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.sagas["saga-1"].step != SagaStepCompensated {
		t.Errorf("step = %q, want COMPENSATED", repo.sagas["saga-1"].step)
	}
	// must transition through COMPENSATING first
	if len(repo.setSteps) < 2 || repo.setSteps[0] != SagaStepCompensating || repo.setSteps[1] != SagaStepCompensated {
		t.Errorf("step sequence = %v, want [COMPENSATING COMPENSATED]", repo.setSteps)
	}
	if len(del.deleted) != 1 || del.deleted[0] != "sub-1" {
		t.Errorf("deleted = %v, want [sub-1]", del.deleted)
	}
}

func TestOnConfirmationEmailFailed_AlreadyCompleted_NoCompensation(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-1", "sub-1", SagaStepCompleted)
	del := &fakeSubDeleter{}
	orch := newOrchestrator(repo, del)

	if err := orch.OnConfirmationEmailFailed(context.Background(), failedEvent("saga-1", "late failure")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.sagas["saga-1"].step != SagaStepCompleted {
		t.Errorf("step changed to %q, want COMPLETED unchanged", repo.sagas["saga-1"].step)
	}
	if len(del.deleted) != 0 {
		t.Errorf("unexpected deletions: %v", del.deleted)
	}
}

func TestOnConfirmationEmailFailed_AlreadyCompensated_NoOp(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-1", "sub-1", SagaStepCompensated)
	del := &fakeSubDeleter{}
	orch := newOrchestrator(repo, del)

	if err := orch.OnConfirmationEmailFailed(context.Background(), failedEvent("saga-1", "duplicate")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(del.deleted) != 0 {
		t.Errorf("unexpected deletions: %v", del.deleted)
	}
	// idempotent: no extra SetStep
	if len(repo.setSteps) != 0 {
		t.Errorf("SetStep called %d times, want 0", len(repo.setSteps))
	}
}

// Stuck in COMPENSATING (e.g. crash between COMPENSATING and COMPENSATED): re-drive completes it.
func TestOnConfirmationEmailFailed_StuckCompensating_Redriven(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-1", "sub-1", SagaStepCompensating)
	del := &fakeSubDeleter{}
	orch := newOrchestrator(repo, del)

	if err := orch.OnConfirmationEmailFailed(context.Background(), failedEvent("saga-1", "retry")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if repo.sagas["saga-1"].step != SagaStepCompensated {
		t.Errorf("step = %q, want COMPENSATED", repo.sagas["saga-1"].step)
	}
	if len(del.deleted) != 1 || del.deleted[0] != "sub-1" {
		t.Errorf("deleted = %v, want [sub-1]", del.deleted)
	}
	// must NOT re-write COMPENSATING (already there)
	for _, s := range repo.setSteps {
		if s == SagaStepCompensating {
			t.Errorf("SetStep wrote COMPENSATING again — should skip when already in that step")
		}
	}
}

// ---- Sweep ----

func TestSweep_StuckAwaitingEmail_Compensated(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-1", "sub-1", SagaStepAwaitingEmail)
	repo.stuckSagas = []SagaRow{{SagaID: "saga-1", SubscriptionID: "sub-1", Step: SagaStepAwaitingEmail}}
	del := &fakeSubDeleter{}
	orch := newOrchestrator(repo, del)

	orch.Sweep(context.Background(), 10*time.Minute)

	if repo.sagas["saga-1"].step != SagaStepCompensated {
		t.Errorf("step = %q, want COMPENSATED", repo.sagas["saga-1"].step)
	}
	if len(del.deleted) != 1 || del.deleted[0] != "sub-1" {
		t.Errorf("deleted = %v, want [sub-1]", del.deleted)
	}
}

func TestSweep_StuckCompensating_Redriven(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-2", "sub-2", SagaStepCompensating)
	repo.stuckSagas = []SagaRow{{SagaID: "saga-2", SubscriptionID: "sub-2", Step: SagaStepCompensating}}
	del := &fakeSubDeleter{}
	orch := newOrchestrator(repo, del)

	orch.Sweep(context.Background(), 10*time.Minute)

	if repo.sagas["saga-2"].step != SagaStepCompensated {
		t.Errorf("step = %q, want COMPENSATED", repo.sagas["saga-2"].step)
	}
	if len(del.deleted) != 1 || del.deleted[0] != "sub-2" {
		t.Errorf("deleted = %v, want [sub-2]", del.deleted)
	}
}

func TestSweep_AlreadyTerminal_NoOp(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-3", "sub-3", SagaStepCompleted)
	repo.stuckSagas = []SagaRow{{SagaID: "saga-3", SubscriptionID: "sub-3", Step: SagaStepCompleted}}
	del := &fakeSubDeleter{}
	orch := newOrchestrator(repo, del)

	orch.Sweep(context.Background(), 10*time.Minute)

	if len(del.deleted) != 0 {
		t.Errorf("unexpected deletions: %v", del.deleted)
	}
	if len(repo.setSteps) != 0 {
		t.Errorf("SetStep called %d times, want 0", len(repo.setSteps))
	}
}

func TestSweep_MultipleStuck_AllProcessed(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("s1", "sub-1", SagaStepAwaitingEmail)
	repo.seed("s2", "sub-2", SagaStepCompensating)
	repo.stuckSagas = []SagaRow{
		{SagaID: "s1", SubscriptionID: "sub-1", Step: SagaStepAwaitingEmail},
		{SagaID: "s2", SubscriptionID: "sub-2", Step: SagaStepCompensating},
	}
	del := &fakeSubDeleter{}
	orch := newOrchestrator(repo, del)

	orch.Sweep(context.Background(), 10*time.Minute)

	if repo.sagas["s1"].step != SagaStepCompensated {
		t.Errorf("s1 step = %q, want COMPENSATED", repo.sagas["s1"].step)
	}
	if repo.sagas["s2"].step != SagaStepCompensated {
		t.Errorf("s2 step = %q, want COMPENSATED", repo.sagas["s2"].step)
	}
	if len(del.deleted) != 2 {
		t.Errorf("deleted count = %d, want 2", len(del.deleted))
	}
}

// ---- compensation delete failure ----

func TestOnConfirmationEmailFailed_DeleteFails_StaysCompensating(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-1", "sub-1", SagaStepAwaitingEmail)
	del := &fakeSubDeleter{err: errors.New("db unreachable")}
	orch := newOrchestrator(repo, del)

	// Delete failure must not surface as an error — the attempt counter is committed.
	if err := orch.OnConfirmationEmailFailed(context.Background(), failedEvent("saga-1", "provider reject")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := repo.sagas["saga-1"]
	if s.step != SagaStepCompensating {
		t.Errorf("step = %q, want COMPENSATING (retry pending)", s.step)
	}
	if s.attempts != 1 {
		t.Errorf("attempts = %d, want 1", s.attempts)
	}
	if len(del.deleted) != 0 {
		t.Errorf("unexpected deletions: %v", del.deleted)
	}
}

func TestOnConfirmationEmailFailed_DeleteFails_DeadLettersAtMaxAttempts(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-1", "sub-1", SagaStepCompensating)
	repo.sagas["saga-1"].attempts = DefaultMaxCompensateAttempts - 1
	del := &fakeSubDeleter{err: errors.New("db unreachable")}
	orch := newOrchestrator(repo, del)

	if err := orch.OnConfirmationEmailFailed(context.Background(), failedEvent("saga-1", "still failing")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	s := repo.sagas["saga-1"]
	if s.step != SagaStepCompensationFailed {
		t.Errorf("step = %q, want COMPENSATION_FAILED", s.step)
	}
	if s.attempts != DefaultMaxCompensateAttempts {
		t.Errorf("attempts = %d, want %d", s.attempts, DefaultMaxCompensateAttempts)
	}
}

func TestOnConfirmationEmailFailed_DeadLettered_NoOp(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-1", "sub-1", SagaStepCompensationFailed)
	del := &fakeSubDeleter{}
	orch := newOrchestrator(repo, del)

	if err := orch.OnConfirmationEmailFailed(context.Background(), failedEvent("saga-1", "retry")); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(del.deleted) != 0 {
		t.Errorf("unexpected deletions: %v", del.deleted)
	}
	if len(repo.setSteps) != 0 {
		t.Errorf("SetStep called %d times, want 0", len(repo.setSteps))
	}
}

func TestSweep_DeleteFails_StaysCompensating(t *testing.T) {
	repo := newFakeSagaRepo()
	repo.seed("saga-1", "sub-1", SagaStepAwaitingEmail)
	repo.stuckSagas = []SagaRow{{SagaID: "saga-1", SubscriptionID: "sub-1", Step: SagaStepAwaitingEmail}}
	del := &fakeSubDeleter{err: errors.New("db unreachable")}
	orch := newOrchestrator(repo, del)

	orch.Sweep(context.Background(), 10*time.Minute)

	s := repo.sagas["saga-1"]
	if s.step != SagaStepCompensating {
		t.Errorf("step = %q, want COMPENSATING (retry pending)", s.step)
	}
	if s.attempts != 1 {
		t.Errorf("attempts = %d, want 1", s.attempts)
	}
	if len(del.deleted) != 0 {
		t.Errorf("unexpected deletions: %v", del.deleted)
	}
}

// ---- wrong event type ----

func TestOnConfirmationEmailSent_WrongType_Error(t *testing.T) {
	orch := newOrchestrator(newFakeSagaRepo(), &fakeSubDeleter{})
	err := orch.OnConfirmationEmailSent(context.Background(), commands.ConfirmationEmailFailed{})
	if err == nil {
		t.Fatal("expected error for wrong event type")
	}
}

func TestOnConfirmationEmailFailed_WrongType_Error(t *testing.T) {
	orch := newOrchestrator(newFakeSagaRepo(), &fakeSubDeleter{})
	err := orch.OnConfirmationEmailFailed(context.Background(), commands.ConfirmationEmailSent{})
	if err == nil {
		t.Fatal("expected error for wrong event type")
	}
}
