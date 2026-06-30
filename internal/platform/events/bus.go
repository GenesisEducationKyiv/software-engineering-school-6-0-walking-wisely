package events

import (
	"context"
	"fmt"
	"sync"
)

// Event is the marker interface for domain events dispatched inside the monolith.
type Event interface {
	EventName() string
}

// Handler reacts to an event published on the local bus.
type Handler func(context.Context, Event) error

// Bus dispatches events to in-process subscribers.
type Bus struct {
	mu       sync.RWMutex
	handlers map[string][]Handler
}

// NewBus returns an empty event bus.
func NewBus() *Bus {
	return &Bus{handlers: make(map[string][]Handler)}
}

// Subscribe registers handler for the given event name.
func (b *Bus) Subscribe(eventName string, handler Handler) {
	b.mu.Lock()
	defer b.mu.Unlock()

	b.handlers[eventName] = append(b.handlers[eventName], handler)
}

// Publish dispatches event to all subscribers synchronously in registration order.
func (b *Bus) Publish(ctx context.Context, event Event) error {
	b.mu.RLock()
	handlers := append([]Handler(nil), b.handlers[event.EventName()]...)
	b.mu.RUnlock()

	for _, handler := range handlers {
		if err := handler(ctx, event); err != nil {
			return fmt.Errorf("handle %s: %w", event.EventName(), err)
		}
	}

	return nil
}
