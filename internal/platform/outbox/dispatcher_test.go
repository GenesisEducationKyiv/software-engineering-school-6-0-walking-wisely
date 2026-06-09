package outbox

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
)

// ── fake event type registered for decode tests ──────────────────────────────

type dispatchTestEvent struct {
	events.Metadata
}

func (dispatchTestEvent) EventName() string     { return "outbox.dispatch_test_event" }
func (dispatchTestEvent) AggregateType() string { return "test" }
func (dispatchTestEvent) AggregateID() string   { return "test-id" }

func init() {
	events.RegisterType(dispatchTestEvent{})
}

// ── fakes ─────────────────────────────────────────────────────────────────────

type fakeDispatchRepo struct {
	records          []Record
	claimErr         error
	markFailedErr    error
	markDeliveredErr error

	markFailedCalls    []markFailedArgs
	markDeliveredCalls []string
}

type markFailedArgs struct {
	id           string
	attemptCount int
	maxAttempts  int
	cause        error
}

func (r *fakeDispatchRepo) ClaimPending(_ context.Context, _ string, _ int) ([]Record, error) {
	return r.records, r.claimErr
}

func (r *fakeDispatchRepo) MarkFailed(_ context.Context, id string, attemptCount, maxAttempts int, cause error) error {
	r.markFailedCalls = append(r.markFailedCalls, markFailedArgs{id, attemptCount, maxAttempts, cause})
	return r.markFailedErr
}

func (r *fakeDispatchRepo) MarkDelivered(_ context.Context, id string) error {
	r.markDeliveredCalls = append(r.markDeliveredCalls, id)
	return r.markDeliveredErr
}

type fakeBus struct {
	publishErr error
	published  []events.Event
}

func (b *fakeBus) Publish(_ context.Context, e events.Event) error {
	b.published = append(b.published, e)
	return b.publishErr
}

type recordingLogger struct {
	errors []string
}

func (l *recordingLogger) Debug(string, ...any) {}
func (l *recordingLogger) Info(string, ...any)  {}
func (l *recordingLogger) Warn(string, ...any)  {}
func (l *recordingLogger) Error(msg string, _ ...any) {
	l.errors = append(l.errors, msg)
}

func (l *recordingLogger) ErrorContext(_ context.Context, msg string, _ ...any) {
	l.errors = append(l.errors, msg)
}

func (l *recordingLogger) hasMessage(msg string) bool {
	for _, e := range l.errors {
		if e == msg {
			return true
		}
	}
	return false
}

// ── helpers ───────────────────────────────────────────────────────────────────

func makeTestRecord(id, eventType string) Record {
	payload, _ := json.Marshal(dispatchTestEvent{
		Metadata: events.Metadata{
			ID:    id,
			At:    time.Now().UTC(),
			V:     1,
			IdKey: "key-" + id,
		},
	})
	return Record{
		ID:           id,
		EventType:    eventType,
		PayloadJSON:  payload,
		AttemptCount: 0,
	}
}

// ── tests ─────────────────────────────────────────────────────────────────────

func TestDispatchBatchHappyPath(t *testing.T) {
	// Arrange
	id := "00000000-0000-0000-0000-000000000001"
	repo := &fakeDispatchRepo{records: []Record{makeTestRecord(id, "outbox.dispatch_test_event")}}
	bus := &fakeBus{}
	log := &recordingLogger{}

	// Act
	err := dispatchBatch(context.Background(), repo, bus, "worker", 10, 5, log)
	// Assert
	if err != nil {
		t.Fatalf("dispatchBatch returned error: %v", err)
	}
	if len(bus.published) != 1 {
		t.Fatalf("published %d events, want 1", len(bus.published))
	}
	if len(repo.markDeliveredCalls) != 1 || repo.markDeliveredCalls[0] != id {
		t.Fatalf("MarkDelivered not called with id %q, got: %v", id, repo.markDeliveredCalls)
	}
	if len(repo.markFailedCalls) != 0 {
		t.Fatalf("MarkFailed called unexpectedly: %v", repo.markFailedCalls)
	}
	if len(log.errors) != 0 {
		t.Fatalf("unexpected error log entries: %v", log.errors)
	}
}

func TestDispatchBatchEmptyBatch(t *testing.T) {
	// Arrange
	repo := &fakeDispatchRepo{records: nil}
	bus := &fakeBus{}

	// Act
	err := dispatchBatch(context.Background(), repo, bus, "worker", 10, 5, &recordingLogger{})
	// Assert
	if err != nil {
		t.Fatalf("expected nil for empty batch, got: %v", err)
	}
	if len(bus.published) != 0 {
		t.Fatalf("expected no publications for empty batch, got %d", len(bus.published))
	}
}

func TestDispatchBatchClaimPendingError(t *testing.T) {
	// Arrange
	repo := &fakeDispatchRepo{claimErr: errors.New("db unavailable")}
	bus := &fakeBus{}

	// Act
	err := dispatchBatch(context.Background(), repo, bus, "worker", 10, 5, &recordingLogger{})

	// Assert
	if err == nil {
		t.Fatal("expected error from ClaimPending propagated, got nil")
	}
}

func TestDispatchBatchDecodeError(t *testing.T) {
	// Arrange
	id := "00000000-0000-0000-0000-000000000001"
	repo := &fakeDispatchRepo{records: []Record{{
		ID:          id,
		EventType:   "outbox.unknown_event_type",
		PayloadJSON: []byte(`{}`),
	}}}
	bus := &fakeBus{}
	log := &recordingLogger{}

	// Act
	err := dispatchBatch(context.Background(), repo, bus, "worker", 10, 5, log)
	// Assert
	if err != nil {
		t.Fatalf("dispatchBatch returned unexpected error: %v", err)
	}
	if len(repo.markFailedCalls) != 1 {
		t.Fatalf("MarkFailed called %d times, want 1", len(repo.markFailedCalls))
	}
	call := repo.markFailedCalls[0]
	if call.id != id {
		t.Errorf("MarkFailed id = %q, want %q", call.id, id)
	}
	if call.attemptCount != 1 {
		t.Errorf("MarkFailed attemptCount = %d, want 1", call.attemptCount)
	}
	if call.maxAttempts != 5 {
		t.Errorf("MarkFailed maxAttempts = %d, want 5", call.maxAttempts)
	}
	if !log.hasMessage("outbox decode failed") {
		t.Fatalf("expected 'outbox decode failed' log entry, got: %v", log.errors)
	}
}

func TestDispatchBatchDecodeErrorMarkFailedErrorIsLogged(t *testing.T) {
	// Arrange
	id := "00000000-0000-0000-0000-000000000001"
	repo := &fakeDispatchRepo{
		records: []Record{{
			ID:          id,
			EventType:   "outbox.unknown_event_type",
			PayloadJSON: []byte(`{}`),
		}},
		markFailedErr: errors.New("mark failed error"),
	}
	log := &recordingLogger{}

	// Act
	_ = dispatchBatch(context.Background(), repo, &fakeBus{}, "worker", 10, 5, log)

	// Assert
	if !log.hasMessage("outbox mark failed after decode error") {
		t.Fatalf("expected 'outbox mark failed after decode error' log entry, got: %v", log.errors)
	}
}

func TestDispatchBatchPublishError(t *testing.T) {
	// Arrange
	id := "00000000-0000-0000-0000-000000000001"
	repo := &fakeDispatchRepo{records: []Record{makeTestRecord(id, "outbox.dispatch_test_event")}}
	bus := &fakeBus{publishErr: errors.New("bus error")}
	log := &recordingLogger{}

	// Act
	err := dispatchBatch(context.Background(), repo, bus, "worker", 10, 5, log)
	// Assert
	if err != nil {
		t.Fatalf("dispatchBatch returned unexpected error: %v", err)
	}
	if len(repo.markFailedCalls) != 1 {
		t.Fatalf("MarkFailed called %d times, want 1", len(repo.markFailedCalls))
	}
	call := repo.markFailedCalls[0]
	if call.id != id {
		t.Errorf("MarkFailed id = %q, want %q", call.id, id)
	}
	if call.attemptCount != 1 {
		t.Errorf("MarkFailed attemptCount = %d, want 1 (record.AttemptCount 0 + 1)", call.attemptCount)
	}
	if call.maxAttempts != 5 {
		t.Errorf("MarkFailed maxAttempts = %d, want 5", call.maxAttempts)
	}
	if !log.hasMessage("outbox handler failed") {
		t.Fatalf("expected 'outbox handler failed' log entry, got: %v", log.errors)
	}
}

func TestDispatchBatchMarkFailedErrorAfterPublishFailureIsLogged(t *testing.T) {
	// Arrange
	id1 := "00000000-0000-0000-0000-000000000001"
	id2 := "00000000-0000-0000-0000-000000000002"
	repo := &fakeDispatchRepo{
		records: []Record{
			makeTestRecord(id1, "outbox.dispatch_test_event"),
			makeTestRecord(id2, "outbox.dispatch_test_event"),
		},
		markFailedErr: errors.New("mark failed error"),
	}
	bus := &fakeBus{publishErr: errors.New("bus error")}
	log := &recordingLogger{}

	// Act
	err := dispatchBatch(context.Background(), repo, bus, "worker", 10, 5, log)
	// Assert
	if err != nil {
		t.Fatalf("dispatchBatch returned unexpected error: %v", err)
	}
	// Loop must continue past the MarkFailed error — both records should be attempted.
	if len(repo.markFailedCalls) != 2 {
		t.Fatalf("MarkFailed called %d times, want 2 (loop must continue)", len(repo.markFailedCalls))
	}
	if !log.hasMessage("outbox mark failed failed") {
		t.Fatalf("expected 'outbox mark failed failed' log entry, got: %v", log.errors)
	}
}

func TestDispatchBatchMarkDeliveredErrorReturnsError(t *testing.T) {
	// Arrange
	id := "00000000-0000-0000-0000-000000000001"
	repo := &fakeDispatchRepo{
		records:          []Record{makeTestRecord(id, "outbox.dispatch_test_event")},
		markDeliveredErr: errors.New("delivered error"),
	}
	bus := &fakeBus{}

	// Act
	err := dispatchBatch(context.Background(), repo, bus, "worker", 10, 5, &recordingLogger{})

	// Assert
	if err == nil {
		t.Fatal("expected error from MarkDelivered to be returned, got nil")
	}
}
