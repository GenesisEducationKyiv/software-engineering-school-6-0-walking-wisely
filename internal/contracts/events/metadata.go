package events

import "time"

// Event is the minimal interface shared by event contracts.
type Event interface {
	EventName() string
}

// Metadata is embedded by durable event contracts.
type Metadata struct {
	ID    string    `json:"event_id"`
	At    time.Time `json:"occurred_at"`
	V     int       `json:"version"`
	IdKey string    `json:"idempotency_key"`
}

func (m Metadata) EventID() string        { return m.ID }
func (m Metadata) OccurredAt() time.Time  { return m.At }
func (m Metadata) Version() int           { return m.V }
func (m Metadata) IdempotencyKey() string { return m.IdKey }

// RegisterTypes registers all concrete event contracts with the caller's codec.
func RegisterTypes(register func(Event)) {
	register(SubscriptionRequested{})
	register(ReleaseDetected{})
}
