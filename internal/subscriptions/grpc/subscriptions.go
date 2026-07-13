package subscriptiongrpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	subscriptionv1 "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

// GetSubscriptions handles GET /api/subscriptions?email=...
// Tokens are never included in the response.
func (s *SubscriptionService) GetSubscriptions(ctx context.Context, req *subscriptionv1.GetSubscriptionsRequest) (*subscriptionv1.GetSubscriptionsResponse, error) {
	subs, err := s.listUseCase.ListByEmail(ctx, req.Email)
	if err != nil {
		if errors.Is(err, domain.ErrInvalidEmail) {
			return nil, status.Error(codes.InvalidArgument, "invalid email format")
		}
		s.log.Error("subscriptions: list failed", "err", err)
		return nil, status.Error(codes.Internal, "internal error")
	}

	resp := make([]*subscriptionv1.Subscription, 0, len(subs))

	for i := range subs {
		s := subs[i]
		lastSeenTag := ""
		if s.LastSeenTag != nil {
			lastSeenTag = *s.LastSeenTag
		}
		resp = append(resp, &subscriptionv1.Subscription{
			Email:       s.Email,
			Repo:        s.Repo,
			Confirmed:   s.Confirmed,
			LastSeenTag: lastSeenTag,
		})
	}

	s.log.Info("subscriptions: listed", "subscriptions_count", len(resp))
	return &subscriptionv1.GetSubscriptionsResponse{Subscriptions: resp}, nil
}
