package outbox

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
)

// ── fakes ──────────────────────────────────────────────────────────────────────

type fakePublisherRepo struct {
	appendErr    error
	appendCalled bool
	lastEvent    events.DurableEvent
}

func (r *fakePublisherRepo) Append(_ context.Context, e events.DurableEvent) error {
	r.appendCalled = true
	r.lastEvent = e
	return r.appendErr
}

// nonDurableEvent implements events.Event but NOT events.DurableEvent.
type nonDurableEvent struct{}

func (nonDurableEvent) EventName() string { return "test.non_durable" }

// durableTestEvent implements events.DurableEvent.
type durableTestEvent struct {
	events.Metadata
}

func (durableTestEvent) EventName() string     { return "test.durable" }
func (durableTestEvent) AggregateType() string { return "test" }
func (durableTestEvent) AggregateID() string   { return "agg-1" }

// ── tests ──────────────────────────────────────────────────────────────────────

func TestPublisherPublishNonDurableReturnsDescriptiveError(t *testing.T) {
	// Arrange
	repo := &fakePublisherRepo{}
	pub := &Publisher{repo: repo}

	// Act
	err := pub.Publish(context.Background(), nonDurableEvent{})

	// Assert
	if err == nil {
		t.Fatal("expected error for non-DurableEvent, got nil")
	}
	if !strings.Contains(err.Error(), "nonDurableEvent") {
		t.Errorf("error message %q should name the offending type", err.Error())
	}
	if repo.appendCalled {
		t.Fatal("Append should not be called for a non-DurableEvent")
	}
}

func TestPublisherPublishDurableDelegatesToRepo(t *testing.T) {
	// Arrange
	repo := &fakePublisherRepo{}
	pub := &Publisher{repo: repo}
	evt := durableTestEvent{
		Metadata: events.Metadata{
			ID:    "00000000-0000-0000-0000-000000000001",
			At:    time.Now().UTC(),
			V:     1,
			IdKey: "key-1",
		},
	}

	// Act
	err := pub.Publish(context.Background(), evt)
	// Assert
	if err != nil {
		t.Fatalf("Publish returned error: %v", err)
	}
	if !repo.appendCalled {
		t.Fatal("Append was not called")
	}
	if repo.lastEvent != evt {
		t.Fatalf("Append received wrong event: got %v, want %v", repo.lastEvent, evt)
	}
}

func TestPublisherPublishPropagatesRepoError(t *testing.T) {
	// Arrange
	repo := &fakePublisherRepo{appendErr: errors.New("db error")}
	pub := &Publisher{repo: repo}
	evt := durableTestEvent{
		Metadata: events.Metadata{
			ID:    "00000000-0000-0000-0000-000000000002",
			At:    time.Now().UTC(),
			V:     1,
			IdKey: "key-2",
		},
	}

	// Act
	err := pub.Publish(context.Background(), evt)

	// Assert
	if err == nil {
		t.Fatal("expected propagated repo error, got nil")
	}
}
