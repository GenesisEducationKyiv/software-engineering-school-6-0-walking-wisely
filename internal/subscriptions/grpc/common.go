// Package subscriptiongrpc adapts subscription use cases to the generated gRPC API.
package subscriptiongrpc

import (
	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
	subscriptionapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app"
)

// SubscriptionTokenWorkflowRepo provides the token-mediated operations needed by subscription use cases.
type SubscriptionTokenWorkflowRepo interface {
	subscriptionapp.SubscriptionWriter
	subscriptionapp.ConfirmationRepo
	subscriptionapp.UnsubscribeRepo
}

// SubscriptionReadRepo reads subscription state for the gRPC API.
type SubscriptionReadRepo = subscriptionapp.SubscriptionReader

// GithubRepoValidator validates that a requested repository exists.
type GithubRepoValidator = subscriptionapp.GithubRepoValidator

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
	subscribeUseCase   *subscriptionapp.SubscribeService
	confirmUseCase     *subscriptionapp.ConfirmService
	unsubscribeUseCase *subscriptionapp.UnsubscribeService
	listUseCase        *subscriptionapp.ListService
}

// NewSubscriptionService constructs a SubscriptionService with the provided dependencies.
func NewSubscriptionService(deps *ServiceDeps) *SubscriptionService {
	return &SubscriptionService{
		subscribeUseCase: subscriptionapp.NewSubscribeService(subscriptionapp.SubscribeDeps{
			Repo:           deps.TokenRepo,
			Github:         deps.Github,
			EmailChan:      deps.EmailChan,
			EmailSecretKey: deps.EmailSecretKey,
			BaseURL:        deps.BaseURL,
		}),
		confirmUseCase:     subscriptionapp.NewConfirmService(deps.TokenRepo),
		unsubscribeUseCase: subscriptionapp.NewUnsubscribeService(deps.TokenRepo),
		listUseCase:        subscriptionapp.NewListService(deps.ReadRepo),
	}
}
