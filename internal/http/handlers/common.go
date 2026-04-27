package handlers

import (
	"context"

	pb "github.com/walking-wisely/genesis2026-github-release-api/gen/subscription/v1"
	"github.com/walking-wisely/genesis2026-github-release-api/internal/domain"
)

type SubRepo interface {
	Subscribe(ctx context.Context, email, repo, confirmToken, unsubToken string) error
	ConfirmByToken(ctx context.Context, token string) (id string, err error)
	UnsubscribeByToken(ctx context.Context, token string) (id string, err error)
	ListByEmail(ctx context.Context, email string) ([]domain.Subscription, error)
	ListDistinctConfirmedRepos(ctx context.Context) ([]string, error)
	ListConfirmedSubscribersForRepo(ctx context.Context, repo string) ([]domain.Subscription, error)
	UpdateLastSeenTag(ctx context.Context, repo, tag string) error
}

type GithubClient interface {
	ValidateRepo(ctx context.Context, repo string) error
}

type ServiceDeps struct {
	SubRepo        SubRepo
	Github         GithubClient
	EmailChan      chan<- domain.EmailMessage
	EmailSecretKey string
	BaseUrl        string
}

type SubscriptionService struct {
	pb.UnimplementedSubscribeServiceServer
	deps ServiceDeps
}

func NewSubscriptionService(deps ServiceDeps) *SubscriptionService {
	return &SubscriptionService{deps: deps}
}
