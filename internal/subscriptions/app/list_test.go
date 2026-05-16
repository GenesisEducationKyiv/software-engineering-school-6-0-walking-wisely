package subscriptionapp

import (
	"context"
	"errors"
	"testing"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

type fakeListRepo struct {
	result []subscriptions.Subscription
	err    error
	email  string
}

func (f *fakeListRepo) ListByEmail(_ context.Context, email string) ([]subscriptions.Subscription, error) {
	f.email = email
	return f.result, f.err
}

func TestListByEmail(t *testing.T) {
	expected := []subscriptions.Subscription{{Email: validEmail, Repo: validRepo}}
	repo := &fakeListRepo{result: expected}
	svc := NewListService(repo)

	got, err := svc.ListByEmail(context.Background(), "  User@Example.COM  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.email != validEmail {
		t.Errorf("repo email = %q, want %q", repo.email, validEmail)
	}
	if len(got) != 1 || got[0].Repo != validRepo {
		t.Errorf("result = %+v, want %+v", got, expected)
	}
}

func TestListByEmail_InvalidEmail(t *testing.T) {
	svc := NewListService(&fakeListRepo{})

	_, err := svc.ListByEmail(context.Background(), "not-an-email")
	if !errors.Is(err, subscriptions.ErrInvalidEmail) {
		t.Errorf("got %v, want ErrInvalidEmail", err)
	}
}

func TestListByEmail_RepoError(t *testing.T) {
	repoErr := errors.New("connection reset by peer")
	svc := NewListService(&fakeListRepo{err: repoErr})

	_, err := svc.ListByEmail(context.Background(), validEmail)
	if !errors.Is(err, repoErr) {
		t.Errorf("got %v, want %v", err, repoErr)
	}
}
