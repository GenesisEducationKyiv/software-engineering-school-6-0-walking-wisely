package handlers

import (
	"context"
	"log/slog"
	"strings"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// GetSubscriptions handles GET /api/subscriptions?email=...
// Tokens are never included in the response.
func (s *SubscriptionService) GetSubscriptions(ctx context.Context, req *pb.GetSubscriptionsRequest) (*pb.GetSubscriptionsResponse, error) {
	email := strings.TrimSpace(strings.ToLower(req.Email))
	if !isValidEmail(email) {
		return nil, status.Error(codes.InvalidArgument, "invalid email format")
	}

	subs, err := s.deps.SubRepo.ListByEmail(ctx, email)
	if err != nil {
		slog.Error("subscriptions: list failed", "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	resp := make([]*pb.Subscription, 0, len(subs))

	for _, s := range subs {
		lastSeenTag := ""
		if s.LastSeenTag != nil {
			lastSeenTag = *s.LastSeenTag
		}
		resp = append(resp, &pb.Subscription{
			Email:       s.Email,
			Repo:        s.Repo,
			Confirmed:   s.Confirmed,
			LastSeenTag: lastSeenTag,
		})
	}

	return &pb.GetSubscriptionsResponse{Subscriptions: resp}, nil
}
