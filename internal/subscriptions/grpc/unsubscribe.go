package subscriptiongrpc

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

// Unsubscribe handles GET /api/unsubscribe/{token}.
// The token used is embedded in every notification email footer, giving subscribers
// a one-click opt-out path that requires no login.
func (s *SubscriptionService) Unsubscribe(ctx context.Context, req *pb.UnsubscribeRequest) (*pb.UnsubscribeResponse, error) {
	token := req.Token
	if !isValidToken(token) {
		return nil, status.Error(codes.InvalidArgument, "invalid token format")
	}

	id, err := s.deps.TokenRepo.UnsubscribeByToken(ctx, token)
	if err != nil {
		if errors.Is(err, subscriptions.ErrTokenNotFound) {
			return nil, status.Error(codes.NotFound, "token not found")
		}
		slog.Error("unsubscribe: db error", "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	slog.Info("unsubscribe: subscription removed", "subscription_id", id)
	return &pb.UnsubscribeResponse{}, nil
}
