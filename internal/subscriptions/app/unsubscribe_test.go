package subscriptionapp

import (
	"context"
	"errors"
	"testing"

	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

type fakeUnsubscribeRepo struct {
	id       string
	err      error
	calls    int
	gotCtx   context.Context
	gotToken string
}

func (f *fakeUnsubscribeRepo) UnsubscribeByToken(ctx context.Context, token string) (string, error) {
	f.calls++
	f.gotCtx = ctx
	f.gotToken = token
	return f.id, f.err
}

func TestUnsubscribe(t *testing.T) {
	repo := &fakeUnsubscribeRepo{id: "sub-42"}
	svc := NewUnsubscribeService(repo)
	ctx := context.WithValue(context.Background(), testContextKey{}, "request-123")

	id, err := svc.Unsubscribe(ctx, validToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "sub-42" {
		t.Errorf("id = %q, want %q", id, "sub-42")
	}
	if repo.calls != 1 {
		t.Fatalf("repo calls = %d, want 1", repo.calls)
	}
	if repo.gotCtx != ctx {
		t.Errorf("repo context was not passed through")
	}
	if repo.gotToken != validToken {
		t.Errorf("repo token = %q, want %q", repo.gotToken, validToken)
	}
}

func TestUnsubscribe_InvalidToken(t *testing.T) {
	repo := &fakeUnsubscribeRepo{}
	svc := NewUnsubscribeService(repo)

	_, err := svc.Unsubscribe(context.Background(), "not-a-valid-token")
	if !errors.Is(err, subscriptionsdomain.ErrInvalidToken) {
		t.Errorf("got %v, want ErrInvalidToken", err)
	}
	if repo.calls != 0 {
		t.Errorf("repo calls = %d, want 0", repo.calls)
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

func TestUnsubscribe_TokenNotFound(t *testing.T) {
	svc := NewUnsubscribeService(&fakeUnsubscribeRepo{err: subscriptionsdomain.ErrTokenNotFound})

	_, err := svc.Unsubscribe(context.Background(), validToken)
	if !errors.Is(err, subscriptionsdomain.ErrTokenNotFound) {
		t.Errorf("got %v, want ErrTokenNotFound", err)
	}
}
