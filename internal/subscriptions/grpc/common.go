// Package subscriptiongrpc adapts subscription use cases to the generated gRPC API.
package subscriptiongrpc

import (
	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
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
	TxManager      subscriptionapp.TransactionManager
	ReadRepo       SubscriptionReadRepo
	Github         GithubRepoValidator
	Publisher      events.Publisher
	EmailSecretKey string
	Log            logger.Logger
}

// SubscriptionService implements the gRPC SubscribeServiceServer interface.
type SubscriptionService struct {
	pb.UnimplementedSubscribeServiceServer
	subscribeUseCase   *subscriptionapp.SubscribeService
	confirmUseCase     *subscriptionapp.ConfirmService
	unsubscribeUseCase *subscriptionapp.UnsubscribeService
	listUseCase        *subscriptionapp.ListService
	log                logger.Logger
}

// NewSubscriptionService constructs a SubscriptionService with the provided dependencies.
func NewSubscriptionService(deps *ServiceDeps) *SubscriptionService {
	log := deps.Log
	if log == nil {
		log = logger.NoopLogger{}
	}

	return &SubscriptionService{
		subscribeUseCase: subscriptionapp.NewSubscribeService(&subscriptionapp.SubscribeDeps{
			Repo:           deps.TokenRepo,
			TxManager:      deps.TxManager,
			Github:         deps.Github,
			Publisher:      deps.Publisher,
			EmailSecretKey: deps.EmailSecretKey,
		}),
		confirmUseCase:     subscriptionapp.NewConfirmService(deps.TokenRepo),
		unsubscribeUseCase: subscriptionapp.NewUnsubscribeService(deps.TokenRepo),
		listUseCase:        subscriptionapp.NewListService(deps.ReadRepo),
		log:                log,
	}
}
