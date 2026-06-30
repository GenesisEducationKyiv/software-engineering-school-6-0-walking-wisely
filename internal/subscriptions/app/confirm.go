package subscriptionapp

import (
	"context"

	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

// ConfirmationRepo stores confirmation-token lifecycle changes.
type ConfirmationRepo interface {
	ConfirmByToken(ctx context.Context, token string) (id string, err error)
}

// ConfirmService coordinates subscription confirmation.
type ConfirmService struct {
	repo ConfirmationRepo
}

// NewConfirmService returns an application service for confirmation.
func NewConfirmService(repo ConfirmationRepo) *ConfirmService {
	return &ConfirmService{repo: repo}
}

// Confirm validates token format and confirms the matching subscription.
func (s *ConfirmService) Confirm(ctx context.Context, token string) (string, error) {
	if !IsValidToken(token) {
		return "", subscriptionsdomain.ErrInvalidToken
	}
	return s.repo.ConfirmByToken(ctx, token)
}
