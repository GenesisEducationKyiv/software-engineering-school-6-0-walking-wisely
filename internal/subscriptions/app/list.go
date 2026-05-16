package subscriptionapp

import (
	"context"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

// SubscriptionReader reads subscription state.
type SubscriptionReader interface {
	ListByEmail(ctx context.Context, email string) ([]subscriptions.Subscription, error)
}

// ListService coordinates subscription lookup.
type ListService struct {
	repo SubscriptionReader
}

// NewListService returns an application service for subscription lookup.
func NewListService(repo SubscriptionReader) *ListService {
	return &ListService{repo: repo}
}

// ListByEmail validates and normalizes the email before reading subscriptions.
func (s *ListService) ListByEmail(ctx context.Context, email string) ([]subscriptions.Subscription, error) {
	email = NormalizeEmail(email)
	if !IsValidEmail(email) {
		return nil, subscriptions.ErrInvalidEmail
	}
	return s.repo.ListByEmail(ctx, email)
}
