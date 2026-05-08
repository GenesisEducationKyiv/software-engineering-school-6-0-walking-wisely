// Package handlers implements the gRPC SubscribeService and wires it to HTTP via grpc-gateway.
package handlers

import (
	"context"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/domain"
)

// SubRepo is the database interface required by the subscription handlers.
type SubRepo interface {
	Subscribe(ctx context.Context, email, repo, confirmToken, unsubToken string) error
	ConfirmByToken(ctx context.Context, token string) (id string, err error)
	UnsubscribeByToken(ctx context.Context, token string) (id string, err error)
	ListByEmail(ctx context.Context, email string) ([]domain.Subscription, error)
	ListDistinctConfirmedRepos(ctx context.Context) ([]string, error)
	ListConfirmedSubscribersForRepo(ctx context.Context, repo string) ([]domain.Subscription, error)
	UpdateLastSeenTag(ctx context.Context, repo, tag string) error
}

// GithubClient is the GitHub API interface used to validate repositories during subscription.
type GithubClient interface {
	ValidateRepo(ctx context.Context, repo string) error
}

// ServiceDeps bundles the external dependencies injected into SubscriptionService.
type ServiceDeps struct {
	SubRepo        SubRepo
	Github         GithubClient
	EmailChan      chan<- domain.EmailMessage
	EmailSecretKey string
	BaseURL        string
}

// SubscriptionService implements the gRPC SubscribeServiceServer interface.
type SubscriptionService struct {
	pb.UnimplementedSubscribeServiceServer
	deps ServiceDeps
}

// NewSubscriptionService constructs a SubscriptionService with the provided dependencies.
func NewSubscriptionService(deps ServiceDeps) *SubscriptionService {
	return &SubscriptionService{deps: deps}
}
