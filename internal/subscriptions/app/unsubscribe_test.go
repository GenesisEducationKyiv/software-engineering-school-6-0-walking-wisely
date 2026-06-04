package subscriptionapp

import (
	"context"
	"errors"
	"testing"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

type fakeUnsubscribeRepo struct {
	id  string
	err error
}

func (f *fakeUnsubscribeRepo) UnsubscribeByToken(_ context.Context, _ string) (string, error) {
	return f.id, f.err
}

func TestUnsubscribe(t *testing.T) {
	repo := &fakeUnsubscribeRepo{id: "sub-42"}
	svc := NewUnsubscribeService(repo)

	id, err := svc.Unsubscribe(context.Background(), validToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "sub-42" {
		t.Errorf("id = %q, want %q", id, "sub-42")
	}
}

func TestUnsubscribe_InvalidToken(t *testing.T) {
	svc := NewUnsubscribeService(&fakeUnsubscribeRepo{})

	_, err := svc.Unsubscribe(context.Background(), "")
	if !errors.Is(err, subscriptions.ErrInvalidToken) {
		t.Errorf("got %v, want ErrInvalidToken", err)
	}
}

func TestUnsubscribe_RepoError(t *testing.T) {
	repoErr := errors.New("connection reset by peer")
	svc := NewUnsubscribeService(&fakeUnsubscribeRepo{err: repoErr})

	_, err := svc.Unsubscribe(context.Background(), validToken)
	if !errors.Is(err, repoErr) {
		t.Errorf("got %v, want %v", err, repoErr)
	}
}
