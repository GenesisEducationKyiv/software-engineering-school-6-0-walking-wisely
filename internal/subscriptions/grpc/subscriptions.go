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

// GetSubscriptions handles GET /api/subscriptions?email=...
// Tokens are never included in the response.
func (s *SubscriptionService) GetSubscriptions(ctx context.Context, req *pb.GetSubscriptionsRequest) (*pb.GetSubscriptionsResponse, error) {
	subs, err := s.listUseCase.ListByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, subscriptions.ErrInvalidEmail) {
			return nil, status.Error(codes.InvalidArgument, "invalid email format")
		}
		slog.Error("subscriptions: list failed", "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	resp := make([]*pb.Subscription, 0, len(subs))

	for i := range subs {
		s := subs[i]
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
