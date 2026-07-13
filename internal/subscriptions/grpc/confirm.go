package subscriptiongrpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	subscriptionv1 "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

// ConfirmSubscription handles GET /api/confirm/{token}.
// The token embedded in the confirmation email is the sole auth credential -
// it is HMAC-SHA256 signed and cannot be guessed without the secret key.
func (s *SubscriptionService) ConfirmSubscription(ctx context.Context, req *subscriptionv1.ConfirmSubscriptionRequest) (*subscriptionv1.ConfirmSubscriptionResponse, error) {
	id, err := s.confirmUseCase.Confirm(ctx, req.Token)
	if err != nil {
		switch {
		case errors.Is(err, domain.ErrInvalidToken):
			return nil, status.Error(codes.InvalidArgument, "invalid token format")
		case errors.Is(err, domain.ErrTokenNotFound):
			return nil, status.Error(codes.NotFound, "token not found")
		}
		s.log.Error("confirm: db error", "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	s.log.Info("confirm: subscription confirmed", "subscription_id", id)
	return &subscriptionv1.ConfirmSubscriptionResponse{}, nil
}
