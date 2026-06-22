// Package mail defines the email delivery contract shared between components.
package mail

import "context"

// Message is a unit of email work queued for delivery.
type Message struct {
	To      string
	Subject string
	HTML    string
}

// Sender delivers email messages.
//
// Implementations may use a provider-native batch API or send messages one by
// one internally. MaxBatchSize tells callers how many messages they should pass
// in a single SendBatch call.
type Sender interface {
	SendBatch(ctx context.Context, messages []Message) error
	MaxBatchSize() int
}
