package subscriptionapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts"
	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

const (
	validEmail = "user@example.com"
	validRepo  = "owner/repo"
)

type fakeSubscriptionRepo struct {
	subscribeErr error
	result       subscriptionsdomain.SubscribeResult
	calls        int
	ctx          context.Context
	email        string
	repo         string
	confirmToken string
	unsubToken   string
}

func (f *fakeSubscriptionRepo) Subscribe(
	ctx context.Context,
	email, repo, confirmToken, unsubToken string,
) (subscriptionsdomain.SubscribeResult, error) {
	f.calls++
	f.ctx = ctx
	f.email = email
	f.repo = repo
	f.confirmToken = confirmToken
	f.unsubToken = unsubToken
	if f.result.SubscriptionID == "" {
		f.result = subscriptionsdomain.SubscribeResult{
			SubscriptionID: "sub-1",
			Action:         subscriptionsdomain.SubscribeActionCreated,
		}
	}
	return f.result, f.subscribeErr
}

type fakeTxManager struct{}

func (fakeTxManager) WithinTransaction(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

type fakeGithubClient struct {
	validateRepoErr error
	calls           int
	ctx             context.Context
	repo            string
}

func (f *fakeGithubClient) ValidateRepo(ctx context.Context, repo string) error {
	f.calls++
	f.ctx = ctx
	f.repo = repo
	return f.validateRepoErr
}

// fakeOrchestrator captures EnqueueWithinTx calls for assertion.
type fakeOrchestrator struct {
	err   error
	calls []enqueueCall
}

type enqueueCall struct {
	subscriptionID string
	email          string
	repo           string
	confirmToken   string
	unsubToken     string
}

func (f *fakeOrchestrator) EnqueueWithinTx(_ context.Context, subscriptionID, email, repo, confirmToken, unsubToken string) error {
	f.calls = append(f.calls, enqueueCall{subscriptionID, email, repo, confirmToken, unsubToken})
	return f.err
}

func newSubscribeService(
	gh GithubRepoValidator,
	repo SubscriptionWriter,
	orch SubscriptionOrchestrator,
) *SubscribeService {
	return NewSubscribeService(&SubscribeDeps{
		Repo:           repo,
		TxManager:      fakeTxManager{},
		Github:         gh,
		Orchestrator:   orch,
		EmailSecretKey: "test-secret",
	})
}

func TestSubscribe(t *testing.T) {
	repo := &fakeSubscriptionRepo{}
	gh := &fakeGithubClient{}
	orch := &fakeOrchestrator{}
	svc := newSubscribeService(gh, repo, orch)
	ctx := context.WithValue(context.Background(), testContextKey{}, "request-123")

	result, err := svc.Subscribe(ctx, SubscribeCommand{
		Email: "  User@Example.COM  ",
		Repo:  "  owner/repo  ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SubscriptionID != "sub-1" || result.Action != subscriptionsdomain.SubscribeActionCreated {
		t.Fatalf("result = %+v, want created sub-1", result)
	}

	if gh.calls != 1 {
		t.Fatalf("github calls = %d, want 1", gh.calls)
	}
	if gh.repo != validRepo {
		t.Errorf("github repo = %q, want %q", gh.repo, validRepo)
	}

	if repo.calls != 1 {
		t.Fatalf("repo calls = %d, want 1", repo.calls)
	}
	if repo.email != validEmail {
		t.Errorf("repo email = %q, want %q", repo.email, validEmail)
	}
	if repo.repo != validRepo {
		t.Errorf("repo repo = %q, want %q", repo.repo, validRepo)
	}
	if !IsValidToken(repo.confirmToken) {
		t.Errorf("confirm token = %q, want valid token", repo.confirmToken)
	}
	if !IsValidToken(repo.unsubToken) {
		t.Errorf("unsubscribe token = %q, want valid token", repo.unsubToken)
	}
	if repo.confirmToken == repo.unsubToken {
		t.Errorf("confirm and unsubscribe tokens should differ")
	}

	if len(orch.calls) != 1 {
		t.Fatalf("orchestrator EnqueueWithinTx called %d times, want 1", len(orch.calls))
	}
	call := orch.calls[0]
	if call.subscriptionID != "sub-1" {
		t.Errorf("enqueue subscriptionID = %q, want sub-1", call.subscriptionID)
	}
	if call.email != validEmail {
		t.Errorf("enqueue email = %q, want %q", call.email, validEmail)
	}
	if call.repo != validRepo {
		t.Errorf("enqueue repo = %q, want %q", call.repo, validRepo)
	}
	if call.confirmToken != repo.confirmToken {
		t.Errorf("enqueue confirmToken = %q, want %q", call.confirmToken, repo.confirmToken)
	}
}

func TestSubscribe_EmailValidation(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr error
	}{
		{"empty string", "", subscriptionsdomain.ErrInvalidEmail},
		{"no at-sign", "notanemail", subscriptionsdomain.ErrInvalidEmail},
		{"multiple at-signs", "a@b@c.com", subscriptionsdomain.ErrInvalidEmail},
		{"empty local part", "@subscriptions.com", subscriptionsdomain.ErrInvalidEmail},
		{"domain without dot", "user@domain", subscriptionsdomain.ErrInvalidEmail},
		{"empty domain", "user@", subscriptionsdomain.ErrInvalidEmail},
		{"valid", validEmail, nil},
		{"trims whitespace", "  user@example.com  ", nil},
		{"lowercases uppercase", "User@Example.COM", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeSubscriptionRepo{}
			gh := &fakeGithubClient{}
			orch := &fakeOrchestrator{}
			svc := newSubscribeService(gh, repo, orch)

			_, err := svc.Subscribe(context.Background(), SubscribeCommand{
				Email: tc.email,
				Repo:  validRepo,
			})

			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if gh.calls != 0 {
					t.Errorf("github calls = %d, want 0", gh.calls)
				}
				if repo.calls != 0 {
					t.Errorf("repo calls = %d, want 0", repo.calls)
				}
			}
		})
	}
}

func TestSubscribe_RepoValidation(t *testing.T) {
	tests := []struct {
		name    string
		repo    string
		wantErr error
	}{
		{"empty string", "", subscriptionsdomain.ErrInvalidRepo},
		{"no slash", "owneronly", subscriptionsdomain.ErrInvalidRepo},
		{"slash only", "/", subscriptionsdomain.ErrInvalidRepo},
		{"empty owner", "/repo", subscriptionsdomain.ErrInvalidRepo},
		{"empty name", "owner/", subscriptionsdomain.ErrInvalidRepo},
		{"space in name", "owner/repo name", subscriptionsdomain.ErrInvalidRepo},
		{"too many slashes", "owner/repo/extra", subscriptionsdomain.ErrInvalidRepo},
		{"trims whitespace", "  owner/repo  ", nil},
		{"allows dots hyphens underscores", "my.org/my-repo_v2", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeSubscriptionRepo{}
			gh := &fakeGithubClient{}
			orch := &fakeOrchestrator{}
			svc := newSubscribeService(gh, repo, orch)

			_, err := svc.Subscribe(context.Background(), SubscribeCommand{
				Email: validEmail,
				Repo:  tc.repo,
			})

			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want %v", err, tc.wantErr)
			}
			if tc.wantErr != nil {
				if gh.calls != 0 {
					t.Errorf("github calls = %d, want 0", gh.calls)
				}
				if repo.calls != 0 {
					t.Errorf("repo calls = %d, want 0", repo.calls)
				}
			}
		})
	}
}

func TestSubscribe_GitHubErrors(t *testing.T) {
	tests := []struct {
		name      string
		githubErr error
	}{
		{"repo not found", contracts.ErrRepoNotFound},
		{"unexpected error", errors.New("connection timeout")},
		{"rate limited", &contracts.RateLimitError{Service: "GitHub", RetryAfter: 30 * time.Second}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeSubscriptionRepo{}
			gh := &fakeGithubClient{validateRepoErr: tc.githubErr}
			orch := &fakeOrchestrator{}
			svc := newSubscribeService(gh, repo, orch)

			_, err := svc.Subscribe(context.Background(), SubscribeCommand{
				Email: validEmail,
				Repo:  validRepo,
			})

			if !errors.Is(err, tc.githubErr) {
				t.Errorf("got %v, want original github error %v", err, tc.githubErr)
			}
			if gh.calls != 1 {
				t.Errorf("github calls = %d, want 1", gh.calls)
			}
			if repo.calls != 0 {
				t.Errorf("repo calls = %d, want 0", repo.calls)
			}
		})
	}
}

func TestSubscribe_TokenRepoErrors(t *testing.T) {
	tests := []struct {
		name         string
		tokenRepoErr error
	}{
		{"already subscribed", subscriptionsdomain.ErrAlreadySubscribed},
		{"unexpected db error", errors.New("connection reset by peer")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeSubscriptionRepo{subscribeErr: tc.tokenRepoErr}
			gh := &fakeGithubClient{}
			orch := &fakeOrchestrator{}
			svc := newSubscribeService(gh, repo, orch)

			_, err := svc.Subscribe(context.Background(), SubscribeCommand{
				Email: validEmail,
				Repo:  validRepo,
			})

			if !errors.Is(err, tc.tokenRepoErr) {
				t.Errorf("got %v, want %v", err, tc.tokenRepoErr)
			}
			if gh.calls != 1 {
				t.Errorf("github calls = %d, want 1", gh.calls)
			}
			if repo.calls != 1 {
				t.Errorf("repo calls = %d, want 1", repo.calls)
			}
		})
	}
}
