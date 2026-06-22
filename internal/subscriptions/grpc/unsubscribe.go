package subscriptiongrpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

// Unsubscribe handles GET /api/unsubscribe/{token}.
// The token used is embedded in every notification email footer, giving subscribers
// a one-click opt-out path that requires no login.
func (s *SubscriptionService) Unsubscribe(ctx context.Context, req *pb.UnsubscribeRequest) (*pb.UnsubscribeResponse, error) {
	id, err := s.unsubscribeUseCase.Unsubscribe(ctx, req.Token)
	if err != nil {
		switch {
		case errors.Is(err, subscriptionsdomain.ErrInvalidToken):
			return nil, status.Error(codes.InvalidArgument, "invalid token format")
		case errors.Is(err, subscriptionsdomain.ErrTokenNotFound):
			return nil, status.Error(codes.NotFound, "token not found")
		}
		s.log.Error("unsubscribe: db error", "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	s.log.Info("unsubscribe: subscription removed", "subscription_id", id)
	return &pb.UnsubscribeResponse{}, nil
}
