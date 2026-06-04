package subscriptionapp

import (
	"context"
	"errors"
	"testing"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

const validToken = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

type fakeConfirmRepo struct {
	id       string
	err      error
	calls    int
	gotCtx   context.Context
	gotToken string
}

func (f *fakeConfirmRepo) ConfirmByToken(ctx context.Context, token string) (string, error) {
	f.calls++
	f.gotCtx = ctx
	f.gotToken = token
	return f.id, f.err
}

func TestConfirm(t *testing.T) {
	repo := &fakeConfirmRepo{id: "sub-123"}
	svc := NewConfirmService(repo)
	ctx := context.WithValue(context.Background(), testContextKey{}, "request-123")

	id, err := svc.Confirm(ctx, validToken)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if id != "sub-123" {
		t.Errorf("id = %q, want %q", id, "sub-123")
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

func TestConfirm_InvalidToken(t *testing.T) {
	repo := &fakeConfirmRepo{}
	svc := NewConfirmService(repo)

	_, err := svc.Confirm(context.Background(), "not-a-valid-token")
	if !errors.Is(err, subscriptions.ErrInvalidToken) {
		t.Errorf("got %v, want ErrInvalidToken", err)
	}
	if repo.calls != 0 {
		t.Errorf("repo calls = %d, want 0", repo.calls)
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

func TestConfirm_TokenNotFound(t *testing.T) {
	svc := NewConfirmService(&fakeConfirmRepo{err: subscriptions.ErrTokenNotFound})

	_, err := svc.Confirm(context.Background(), validToken)
	if !errors.Is(err, subscriptions.ErrTokenNotFound) {
		t.Errorf("got %v, want ErrTokenNotFound", err)
	}
}

type testContextKey struct{}
