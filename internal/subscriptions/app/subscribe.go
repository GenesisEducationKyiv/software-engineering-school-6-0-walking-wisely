// Package subscriptionapp contains subscription application use cases.
package subscriptionapp

import (
	"context"
	"fmt"
	"regexp"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

var repoPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+$`)

// SubscriptionWriter persists token-mediated subscription lifecycle changes.
type SubscriptionWriter interface {
	Subscribe(ctx context.Context, email, repo, confirmToken, unsubToken string) error
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
	notifier       *ConfirmationNotifier
	emailSecretKey string
}

// SubscribeDeps bundles the dependencies needed by SubscribeService.
type SubscribeDeps struct {
	Repo           SubscriptionWriter
	Github         GithubRepoValidator
	EmailChan      chan<- mail.Message
	EmailSecretKey string
	BaseURL        string
}

// NewSubscribeService returns an application service for the subscribe workflow.
func NewSubscribeService(deps SubscribeDeps) *SubscribeService {
	return &SubscribeService{
		repo:           deps.Repo,
		github:         deps.Github,
		notifier:       NewConfirmationNotifier(deps.EmailChan, deps.BaseURL),
		emailSecretKey: deps.EmailSecretKey,
	}
}

// Subscribe validates the command, verifies the repo, persists the subscription,
// and requests a confirmation email.
func (s *SubscribeService) Subscribe(ctx context.Context, cmd SubscribeCommand) error {
	email := NormalizeEmail(cmd.Email)
	repo := NormalizeRepo(cmd.Repo)

	if !IsValidEmail(email) {
		return subscriptions.ErrInvalidEmail
	}

	if !IsValidRepo(repo) {
		return subscriptions.ErrInvalidRepo
	}

	if err := s.github.ValidateRepo(ctx, repo); err != nil {
		return err
	}

	confirmToken, err := subscriptions.GenerateToken(s.emailSecretKey)
	if err != nil {
		return fmt.Errorf("generate confirm token: %w", err)
	}
	unsubToken, err := subscriptions.GenerateToken(s.emailSecretKey)
	if err != nil {
		return fmt.Errorf("generate unsub token: %w", err)
	}

	if err := s.repo.Subscribe(ctx, email, repo, confirmToken, unsubToken); err != nil {
		return err
	}

	s.notifier.EnqueueConfirmation(Confirmation{
		Email:        email,
		Repo:         repo,
		ConfirmToken: confirmToken,
		UnsubToken:   unsubToken,
	})

	return nil
}
