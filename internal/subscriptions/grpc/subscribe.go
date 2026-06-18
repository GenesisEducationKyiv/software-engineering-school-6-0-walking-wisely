package subscriptiongrpc

import (
	"context"
	"errors"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
	subscriptionapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app"
)

// Subscribe handles POST /api/subscribe and adapts gRPC requests to the
// subscription application use case.
func (s *SubscriptionService) Subscribe(
	ctx context.Context,
	req *pb.SubscribeRequest,
) (*pb.SubscribeResponse, error) {
	result, err := s.subscribeUseCase.Subscribe(ctx, subscriptionapp.SubscribeCommand{
		Email: req.Email,
		Repo:  req.Repo,
	})
	if err != nil {
		return nil, s.mapSubscribeError(ctx, req.Repo, err)
	}

	s.log.Info("subscribe: completed",
		"subscription_id", result.SubscriptionID,
		"repo", subscriptionapp.NormalizeRepo(req.Repo),
		"action", result.Action)
	return &pb.SubscribeResponse{}, nil
}

func (s *SubscriptionService) mapSubscribeError(ctx context.Context, repo string, err error) error {
	switch {
	case errors.Is(err, subscriptions.ErrInvalidEmail):
		return status.Error(codes.InvalidArgument, "invalid email format")
	case errors.Is(err, subscriptions.ErrInvalidRepo):
		return status.Error(codes.InvalidArgument, "invalid repo format, expected owner/repo")
	case errors.Is(err, contracts.ErrRepoNotFound):
		return status.Error(codes.NotFound, "repository not found on GitHub")
	case errors.Is(err, subscriptions.ErrAlreadySubscribed):
		return status.Error(codes.AlreadyExists, "email already subscribed to this repository")
	}

	var rle *contracts.RateLimitError
	if errors.As(err, &rle) {
		return s.handleRateLimitError(ctx, rle)
	}

	s.log.Error("subscribe: use case failed", "repo", subscriptionapp.NormalizeRepo(repo), "err", err)
	return status.Error(codes.Internal, "internal error")
}
