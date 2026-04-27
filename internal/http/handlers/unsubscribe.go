package handlers

import (
	"context"
	"errors"
	"log/slog"

	pb "github.com/walking-wisely/genesis2026-github-release-api/gen/subscription/v1"
	"github.com/walking-wisely/genesis2026-github-release-api/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Unsubscribe handles GET /api/unsubscribe/{token}.
// The token used is embedded in every notification email footer, giving subscribers
// a one-click opt-out path that requires no login.
func (s *SubscriptionService) Unsubscribe(ctx context.Context, req *pb.UnsubscribeRequest) (*pb.UnsubscribeResponse, error) {
	token := req.Token
	if !isValidToken(token) {
		return nil, status.Error(codes.InvalidArgument, "invalid token format")
	}

	id, err := s.deps.SubRepo.UnsubscribeByToken(ctx, token)
	if err != nil {
		if errors.Is(err, domain.ErrTokenNotFound) {
			return nil, status.Error(codes.NotFound, "token not found")
		}
		slog.Error("unsubscribe: db error", "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	slog.Info("unsubscribe: subscription removed", "subscription_id", id)
	return &pb.UnsubscribeResponse{}, nil
}
