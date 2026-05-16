package subscriptionapp

import (
	"context"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

// UnsubscribeRepo stores unsubscribe-token lifecycle changes.
type UnsubscribeRepo interface {
	UnsubscribeByToken(ctx context.Context, token string) (id string, err error)
}

// UnsubscribeService coordinates one-click subscription removal.
type UnsubscribeService struct {
	repo UnsubscribeRepo
}

// NewUnsubscribeService returns an application service for unsubscribe.
func NewUnsubscribeService(repo UnsubscribeRepo) *UnsubscribeService {
	return &UnsubscribeService{repo: repo}
}

// Unsubscribe validates token format and removes the matching subscription.
func (s *UnsubscribeService) Unsubscribe(ctx context.Context, token string) (string, error) {
	if !IsValidToken(token) {
		return "", subscriptions.ErrInvalidToken
	}
	return s.repo.UnsubscribeByToken(ctx, token)
}
