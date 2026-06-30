package subscriptionapp

import (
	"context"
	"errors"
	"reflect"
	"testing"

	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

type fakeListRepo struct {
	result []subscriptionsdomain.Subscription
	err    error
	calls  int
	ctx    context.Context
	email  string
}

func (f *fakeListRepo) ListByEmail(ctx context.Context, email string) ([]subscriptionsdomain.Subscription, error) {
	f.calls++
	f.ctx = ctx
	f.email = email
	return f.result, f.err
}

func TestListByEmail(t *testing.T) {
	expected := []subscriptionsdomain.Subscription{{Email: validEmail, Repo: validRepo}}
	repo := &fakeListRepo{result: expected}
	svc := NewListService(repo)
	ctx := context.WithValue(context.Background(), testContextKey{}, "request-123")

	got, err := svc.ListByEmail(ctx, "  User@Example.COM  ")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.calls != 1 {
		t.Fatalf("repo calls = %d, want 1", repo.calls)
	}
	if repo.ctx != ctx {
		t.Errorf("repo context was not passed through")
	}
	if repo.email != validEmail {
		t.Errorf("repo email = %q, want %q", repo.email, validEmail)
	}
	if !reflect.DeepEqual(got, expected) {
		t.Errorf("result = %+v, want %+v", got, expected)
	}
}

func TestListByEmail_InvalidEmail(t *testing.T) {
	tests := []struct {
		name  string
		email string
	}{
		{"empty string", ""},
		{"no at-sign", "not-an-email"},
		{"domain without dot", "user@domain"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeListRepo{}
			svc := NewListService(repo)

			_, err := svc.ListByEmail(context.Background(), tc.email)
			if !errors.Is(err, subscriptionsdomain.ErrInvalidEmail) {
				t.Errorf("got %v, want ErrInvalidEmail", err)
			}
			if repo.calls != 0 {
				t.Errorf("repo calls = %d, want 0", repo.calls)
			}
		})
	}
}

func TestListByEmail_EmptyResult(t *testing.T) {
	repo := &fakeListRepo{result: []subscriptionsdomain.Subscription{}}
	svc := NewListService(repo)

	got, err := svc.ListByEmail(context.Background(), validEmail)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got == nil {
		t.Fatalf("result = nil, want empty slice from repo")
	}
	if len(got) != 0 {
		t.Errorf("result = %+v, want empty slice", got)
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
