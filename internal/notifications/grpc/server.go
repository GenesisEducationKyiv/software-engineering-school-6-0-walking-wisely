package notificationgrpc

import (
	"context"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	notificationv1 "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/notification/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/commands"
	contractevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
)

// ConfirmationHandler reacts to a SendConfirmationEmail command by recording a durable job.
type ConfirmationHandler interface {
	OnSendConfirmationEmail(ctx context.Context, event events.Event) error
}

// Server exposes the internal NotificationService gRPC endpoint.
type Server struct {
	notificationv1.UnimplementedNotificationServiceServer
	handlers ConfirmationHandler
}

// NewServer returns a Server backed by the given handler.
func NewServer(handlers ConfirmationHandler) *Server {
	return &Server{handlers: handlers}
}

// SendConfirmation handles an inbound gRPC call and delegates to the existing handler.
func (s *Server) SendConfirmation(ctx context.Context, req *notificationv1.SendConfirmationRequest) (*notificationv1.Ack, error) {
	cmd := commands.SendConfirmationEmail{
		Metadata: contractevents.Metadata{
			ID:    req.EventId,
			At:    time.Now().UTC(),
			V:     1,
			IdKey: req.IdempotencyKey,
		},
		SagaID:         req.SagaId,
		SubscriptionID: req.SubscriptionId,
		Email:          req.Email,
		Repo:           req.Repo,
		ConfirmToken:   req.ConfirmToken,
		UnsubToken:     req.UnsubToken,
	}
	if err := s.handlers.OnSendConfirmationEmail(ctx, cmd); err != nil {
		return nil, status.Errorf(codes.Internal, "record confirmation: %v", err)
	}
	return &notificationv1.Ack{}, nil
}
