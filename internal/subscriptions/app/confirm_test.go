package subscriptionapp

import (
	"context"
	"errors"
	"testing"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

const validToken = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

type fakeConfirmRepo struct {
	id  string
	err error
}

func (f *fakeConfirmRepo) ConfirmByToken(_ context.Context, _ string) (string, error) {
	return f.id, f.err
}

func TestConfirm(t *testing.T) {
	repo := &fakeConfirmRepo{id: "sub-123"}
	svc := NewConfirmService(repo)

	id, err := svc.Confirm(context.Background(), validToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "sub-123" {
		t.Errorf("id = %q, want %q", id, "sub-123")
	}
}

func TestConfirm_InvalidToken(t *testing.T) {
	svc := NewConfirmService(&fakeConfirmRepo{})

	_, err := svc.Confirm(context.Background(), "not-a-valid-token")
	if !errors.Is(err, subscriptions.ErrInvalidToken) {
		t.Errorf("got %v, want ErrInvalidToken", err)
	}
}

func TestConfirm_RepoError(t *testing.T) {
	repoErr := errors.New("connection refused")
	svc := NewConfirmService(&fakeConfirmRepo{err: repoErr})

	_, err := svc.Confirm(context.Background(), validToken)
	if !errors.Is(err, repoErr) {
		t.Errorf("got %v, want %v", err, repoErr)
	}
}
