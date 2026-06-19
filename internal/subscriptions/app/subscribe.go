// Package subscriptionapp contains subscription application use cases.
package subscriptionapp

import (
	"context"
	"fmt"
	"regexp"

	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

var repoPattern = regexp.MustCompile(`^[a-zA-Z0-9._-]+/[a-zA-Z0-9._-]+$`)

// SubscriptionWriter persists token-mediated subscription lifecycle changes.
type SubscriptionWriter interface {
	Subscribe(ctx context.Context, email, repo, confirmToken, unsubToken string) (subscriptionsdomain.SubscribeResult, error)
}

type TransactionManager interface {
	WithinTransaction(ctx context.Context, fn func(context.Context) error) error
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

// SubscriptionOrchestrator enqueues the saga command within an active transaction.
type SubscriptionOrchestrator interface {
	EnqueueWithinTx(txCtx context.Context, subscriptionID, email, repo, confirmToken, unsubToken string) error
}

// SubscribeService coordinates the subscribe use case.
type SubscribeService struct {
	repo           SubscriptionWriter
	txManager      TransactionManager
	github         GithubRepoValidator
	orchestrator   SubscriptionOrchestrator
	emailSecretKey string
}

// SubscribeDeps bundles the dependencies needed by SubscribeService.
type SubscribeDeps struct {
	Repo           SubscriptionWriter
	TxManager      TransactionManager
	Github         GithubRepoValidator
	Orchestrator   SubscriptionOrchestrator
	EmailSecretKey string
}

// NewSubscribeService returns an application service for the subscribe workflow.
func NewSubscribeService(deps *SubscribeDeps) *SubscribeService {
	return &SubscribeService{
		repo:           deps.Repo,
		txManager:      deps.TxManager,
		github:         deps.Github,
		orchestrator:   deps.Orchestrator,
		emailSecretKey: deps.EmailSecretKey,
	}
}

// Subscribe validates the command, verifies the repo, persists the subscription,
// and enqueues the saga's SendConfirmationEmail command — all atomically.
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

	if s.txManager == nil {
		return subscriptionsdomain.SubscribeResult{}, fmt.Errorf("subscribe: transaction manager is required")
	}

	var result subscriptionsdomain.SubscribeResult
	if err := s.txManager.WithinTransaction(ctx, func(txCtx context.Context) error {
		result, err = s.repo.Subscribe(txCtx, email, repo, confirmToken, unsubToken)
		if err != nil {
			return err
		}
		if s.orchestrator == nil {
			return nil
		}
		if err := s.orchestrator.EnqueueWithinTx(txCtx, result.SubscriptionID, email, repo, confirmToken, unsubToken); err != nil {
			return fmt.Errorf("enqueue saga: %w", err)
		}
		return nil
	}); err != nil {
		return subscriptionsdomain.SubscribeResult{}, err
	}

	return result, nil
}
