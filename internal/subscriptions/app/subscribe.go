// Package subscriptionapp contains subscription application use cases.
package subscriptionapp

import (
	"context"
	"fmt"
	"regexp"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

var repoPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+$`)

// SubscriptionWriter persists token-mediated subscription lifecycle changes.
type SubscriptionWriter interface {
	Subscribe(ctx context.Context, email, repo, confirmToken, unsubToken string) (subscriptionsdomain.SubscribeResult, error)
}

// GithubRepoValidator validates that a requested repository exists.
type GithubRepoValidator interface {
	ValidateRepo(ctx context.Context, repo string) error
}

// SubscribeCommand is the input for the subscribe use case.
type SubscribeCommand struct {
	Email string
	Repo  string
}

// SubscribeService coordinates the subscribe use case.
type SubscribeService struct {
	repo           SubscriptionWriter
	github         GithubRepoValidator
	publisher      *events.Bus
	emailSecretKey string
}

// SubscribeDeps bundles the dependencies needed by SubscribeService.
type SubscribeDeps struct {
	Repo           SubscriptionWriter
	Github         GithubRepoValidator
	Publisher      *events.Bus
	EmailSecretKey string
}

// NewSubscribeService returns an application service for the subscribe workflow.
func NewSubscribeService(deps *SubscribeDeps) *SubscribeService {
	return &SubscribeService{
		repo:           deps.Repo,
		github:         deps.Github,
		publisher:      deps.Publisher,
		emailSecretKey: deps.EmailSecretKey,
	}
}

// Subscribe validates the command, verifies the repo, persists the subscription,
// and requests a confirmation email.
func (s *SubscribeService) Subscribe(ctx context.Context, cmd SubscribeCommand) (subscriptionsdomain.SubscribeResult, error) {
	email := NormalizeEmail(cmd.Email)
	repo := NormalizeRepo(cmd.Repo)

	if !IsValidEmail(email) {
		return subscriptionsdomain.SubscribeResult{}, subscriptionsdomain.ErrInvalidEmail
	}

	if !IsValidRepo(repo) {
		return subscriptionsdomain.SubscribeResult{}, subscriptionsdomain.ErrInvalidRepo
	}

	if err := s.github.ValidateRepo(ctx, repo); err != nil {
		return subscriptionsdomain.SubscribeResult{}, err
	}

	confirmToken, err := GenerateToken(s.emailSecretKey)
	if err != nil {
		return subscriptionsdomain.SubscribeResult{}, fmt.Errorf("generate confirm token: %w", err)
	}
	unsubToken, err := GenerateToken(s.emailSecretKey)
	if err != nil {
		return subscriptionsdomain.SubscribeResult{}, fmt.Errorf("generate unsub token: %w", err)
	}

	result, err := s.repo.Subscribe(ctx, email, repo, confirmToken, unsubToken)
	if err != nil {
		return subscriptionsdomain.SubscribeResult{}, err
	}

	if s.publisher != nil {
		if err := s.publisher.Publish(ctx, SubscriptionRequested{
			SubscriptionID: result.SubscriptionID,
			Email:          email,
			Repo:           repo,
			ConfirmToken:   confirmToken,
			UnsubToken:     unsubToken,
		}); err != nil {
			return subscriptionsdomain.SubscribeResult{}, fmt.Errorf("publish subscription requested: %w", err)
		}
	}

	return result, nil
}
