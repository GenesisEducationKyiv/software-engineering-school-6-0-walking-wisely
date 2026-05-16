// Package subscriptiongrpc adapts subscription use cases to the generated gRPC API.
package subscriptiongrpc

import (
	"context"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

// SubscriptionTokenWorkflowRepo stores token-mediated subscription lifecycle changes.
type SubscriptionTokenWorkflowRepo interface {
	Subscribe(ctx context.Context, email, repo, confirmToken, unsubToken string) error
	ConfirmByToken(ctx context.Context, token string) (id string, err error)
	UnsubscribeByToken(ctx context.Context, token string) (id string, err error)
}

// SubscriptionReadRepo reads subscription state for the gRPC API.
type SubscriptionReadRepo interface {
	ListByEmail(ctx context.Context, email string) ([]subscriptions.Subscription, error)
}

// GithubRepoValidator validates that a requested repository exists.
type GithubRepoValidator interface {
	ValidateRepo(ctx context.Context, repo string) error
}

// ServiceDeps bundles the external dependencies injected into SubscriptionService.
type ServiceDeps struct {
	TokenRepo      SubscriptionTokenWorkflowRepo
	ReadRepo       SubscriptionReadRepo
	Github         GithubRepoValidator
	EmailChan      chan<- mail.Message
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
