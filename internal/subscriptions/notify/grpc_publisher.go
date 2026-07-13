// Package notify provides outbound adapters for delivering saga commands to the
// notifications service. The gRPC publisher is a direct-call alternative to the
// NATS JetStream publisher; the outbox still provides at-least-once delivery for
// both transports.
package notify

import (
	"context"
	"fmt"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notificationv1 "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/notification/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/commands"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
)

// GRPCPublisher implements events.Publisher by calling the Notifications gRPC service
// directly for SendConfirmationEmail commands.
//
// A semaphore bounds concurrency so that a recovering Notifications service is not
// thundering-herded: the outbox dispatcher will back off via MarkFailed/retry instead
// of piling up goroutines.
type GRPCPublisher struct {
	client notificationv1.NotificationServiceClient
	sem    chan struct{}
}

// NewGRPCPublisher returns a GRPCPublisher with the given concurrency cap.
func NewGRPCPublisher(client notificationv1.NotificationServiceClient, maxConcurrency int) *GRPCPublisher {
	if maxConcurrency < 1 {
		maxConcurrency = 32
	}
	return &GRPCPublisher{
		client: client,
		sem:    make(chan struct{}, maxConcurrency),
	}
}

// Publish sends the event to the Notifications service over gRPC.
// Only commands.SendConfirmationEmail is handled; any other type returns an error
// so the outbox marks the row failed rather than silently dropping it.
func (p *GRPCPublisher) Publish(ctx context.Context, event events.Event) error {
	cmd, ok := event.(commands.SendConfirmationEmail)
	if !ok {
		return fmt.Errorf("grpc publisher: unexpected event type %T", event)
	}

	select {
	case p.sem <- struct{}{}:
		defer func() { <-p.sem }()
	case <-ctx.Done():
		return ctx.Err()
	}

	_, err := p.client.SendConfirmation(ctx, &notificationv1.SendConfirmationRequest{
		SagaId:         cmd.SagaID,
		SubscriptionId: cmd.SubscriptionID,
		Email:          cmd.Email,
		Repo:           cmd.Repo,
		ConfirmToken:   cmd.ConfirmToken,
		UnsubToken:     cmd.UnsubToken,
		EventId:        cmd.EventID(),
		IdempotencyKey: cmd.IdempotencyKey(),
	})
	if err != nil {
		st, ok := status.FromError(err)
		if ok && (st.Code() == codes.ResourceExhausted || st.Code() == codes.Unavailable) {
			// Transient — outbox will retry with exponential backoff.
			return fmt.Errorf("notifications grpc transient (%s): %w", st.Code(), err)
		}
		return fmt.Errorf("notifications grpc send confirmation: %w", err)
	}
	return nil
}

// RoutingPublisher routes SendConfirmationEmail to a saga publisher (e.g. gRPC) and
// all other events to a fallback publisher (e.g. NATS). This lets the outbox
// dispatcher use a single Publisher while sending saga commands over gRPC.
type RoutingPublisher struct {
	saga     events.Publisher
	fallback events.Publisher
}

// NewRoutingPublisher returns a RoutingPublisher.
func NewRoutingPublisher(saga, fallback events.Publisher) *RoutingPublisher {
	return &RoutingPublisher{saga: saga, fallback: fallback}
}

func (r *RoutingPublisher) Publish(ctx context.Context, event events.Event) error {
	if _, ok := event.(commands.SendConfirmationEmail); ok {
		return r.saga.Publish(ctx, event)
	}
	return r.fallback.Publish(ctx, event)
}
