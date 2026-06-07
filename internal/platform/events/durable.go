package events

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"sync"
	"time"
)

// Publisher is the application-facing event publication port.
type Publisher interface {
	Publish(ctx context.Context, event Event) error
}

// DurableEvent carries the stable envelope metadata needed for outbox delivery.
type DurableEvent interface {
	Event
	AggregateType() string
	AggregateID() string
	EventID() string
	OccurredAt() time.Time
	Version() int
	IdempotencyKey() string
}

// Metadata is embedded by durable event payloads.
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

var (
	codecMu sync.RWMutex
	codecs  = make(map[string]reflect.Type)
)

// RegisterType registers a concrete event type for payload decoding.
func RegisterType(event Event) {
	t := reflect.TypeOf(event)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}

	codecMu.Lock()
	defer codecMu.Unlock()
	codecs[event.EventName()] = t
}

// Decode reconstructs a registered concrete event from JSON payload.
func Decode(eventType string, payload []byte) (Event, error) {
	codecMu.RLock()
	t, ok := codecs[eventType]
	codecMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("no codec registered for %s", eventType)
	}

	value := reflect.New(t)
	if err := json.Unmarshal(payload, value.Interface()); err != nil {
		return nil, fmt.Errorf("unmarshal %s: %w", eventType, err)
	}

	if event, ok := value.Interface().(Event); ok {
		return event, nil
	}

	event, ok := value.Elem().Interface().(Event)
	if !ok {
		return nil, fmt.Errorf("decoded value for %s does not implement Event", eventType)
	}
	return event, nil
}
