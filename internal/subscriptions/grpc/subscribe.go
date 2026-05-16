package subscriptiongrpc

import (
	"context"
	"errors"
	"log/slog"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
	subscriptionapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app"
)

// Subscribe handles POST /api/subscribe and adapts gRPC requests to the
// subscription application use case.
func (s *SubscriptionService) Subscribe(
	ctx context.Context,
	req *pb.SubscribeRequest,
) (*pb.SubscribeResponse, error) {
	if err := s.subscribeUseCase.Subscribe(ctx, subscriptionapp.SubscribeCommand{
		Email: req.Email,
		Repo:  req.Repo,
	}); err != nil {
		return nil, mapSubscribeError(ctx, req.Repo, err)
	}

	slog.Info("subscribe: subscription created", "repo", subscriptionapp.NormalizeRepo(req.Repo))
	return &pb.SubscribeResponse{}, nil
}

func mapSubscribeError(ctx context.Context, repo string, err error) error {
	switch {
	case errors.Is(err, subscriptions.ErrInvalidEmail):
		return status.Error(codes.InvalidArgument, "invalid email format")
	case errors.Is(err, subscriptions.ErrInvalidRepo):
		return status.Error(codes.InvalidArgument, "invalid repo format, expected owner/repo")
	case errors.Is(err, subscriptions.ErrRepoNotFound):
		return status.Error(codes.NotFound, "repository not found on GitHub")
	case errors.Is(err, subscriptions.ErrAlreadySubscribed):
		return status.Error(codes.AlreadyExists, "email already subscribed to this repository")
	}

	var rle *subscriptions.RateLimitError
	if errors.As(err, &rle) {
		return handleRateLimitError(ctx, rle)
	}

	slog.Error("subscribe: use case failed", "repo", subscriptionapp.NormalizeRepo(repo), "err", err)
	return status.Error(codes.Internal, "internal error")
}
