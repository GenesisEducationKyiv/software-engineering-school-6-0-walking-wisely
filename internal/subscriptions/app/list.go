package subscriptionapp

import (
	"context"

	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

// SubscriptionReader reads subscription state.
type SubscriptionReader interface {
	ListByEmail(ctx context.Context, email string) ([]subscriptionsdomain.Subscription, error)
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
func (s *ListService) ListByEmail(ctx context.Context, email string) ([]subscriptionsdomain.Subscription, error) {
	email = NormalizeEmail(email)
	if !IsValidEmail(email) {
		return nil, subscriptionsdomain.ErrInvalidEmail
	}
	return s.repo.ListByEmail(ctx, email)
}
