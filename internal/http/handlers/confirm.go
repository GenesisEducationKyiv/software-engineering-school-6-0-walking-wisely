package handlers

import (
	"context"
	"errors"
	"log/slog"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Confirm handles GET /api/confirm/{token}.
// The token embedded in the confirmation email is the sole auth credential -
// it is HMAC-SHA256 signed and cannot be guessed without the secret key.
func (s *SubscriptionService) ConfirmSubscription(ctx context.Context, req *pb.ConfirmSubscriptionRequest) (*pb.ConfirmSubscriptionResponse, error) {
	token := req.Token
	if !isValidToken(token) {
		return nil, status.Error(codes.InvalidArgument, "invalid token format")
	}

	id, err := s.deps.SubRepo.ConfirmByToken(ctx, token)
	if err != nil {
		if errors.Is(err, domain.ErrTokenNotFound) {
			return nil, status.Error(codes.NotFound, "token not found")
		}
		slog.Error("confirm: db error", "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	slog.Info("confirm: subscription confirmed", "subscription_id", id)
	return &pb.ConfirmSubscriptionResponse{}, nil
}
