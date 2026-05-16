package subscriptionapp

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

const (
	validEmail = "user@example.com"
	validRepo  = "owner/repo"
)

type fakeSubscriptionRepo struct {
	subscribeErr error
}

func (f *fakeSubscriptionRepo) Subscribe(_ context.Context, _, _, _, _ string) error {
	return f.subscribeErr
}

type fakeGithubClient struct {
	validateRepoErr error
}

func (f *fakeGithubClient) ValidateRepo(_ context.Context, _ string) error {
	return f.validateRepoErr
}

func newSubscribeService(
	gh GithubRepoValidator,
	repo SubscriptionWriter,
	ch chan mail.Message,
) *SubscribeService {
	return NewSubscribeService(SubscribeDeps{
		Repo:           repo,
		Github:         gh,
		EmailChan:      ch,
		EmailSecretKey: "test-secret",
		BaseURL:        "http://localhost",
	})
}

func TestSubscribe_EmailValidation(t *testing.T) {
	tests := []struct {
		name    string
		email   string
		wantErr error
	}{
		{"empty string", "", subscriptions.ErrInvalidEmail},
		{"no at-sign", "notanemail", subscriptions.ErrInvalidEmail},
		{"multiple at-signs", "a@b@c.com", subscriptions.ErrInvalidEmail},
		{"empty local part", "@subscriptions.com", subscriptions.ErrInvalidEmail},
		{"domain without dot", "user@domain", subscriptions.ErrInvalidEmail},
		{"empty domain", "user@", subscriptions.ErrInvalidEmail},
		{"valid", validEmail, nil},
		{"trims whitespace", "  user@example.com  ", nil},
		{"lowercases uppercase", "User@Example.COM", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan mail.Message, 1)
			repo := &fakeSubscriptionRepo{}
			svc := newSubscribeService(&fakeGithubClient{}, repo, ch)

			err := svc.Subscribe(context.Background(), SubscribeCommand{
				Email: tc.email,
				Repo:  validRepo,
			})

			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want %v", err, tc.wantErr)
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
		{"empty string", "", subscriptions.ErrInvalidRepo},
		{"no slash", "owneronly", subscriptions.ErrInvalidRepo},
		{"slash only", "/", subscriptions.ErrInvalidRepo},
		{"empty owner", "/repo", subscriptions.ErrInvalidRepo},
		{"empty name", "owner/", subscriptions.ErrInvalidRepo},
		{"space in name", "owner/repo name", subscriptions.ErrInvalidRepo},
		{"too many slashes", "owner/repo/extra", subscriptions.ErrInvalidRepo},
		{"trims whitespace", "  owner/repo  ", nil},
		{"allows dots hyphens underscores", "my.org/my-repo_v2", nil},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan mail.Message, 1)
			repo := &fakeSubscriptionRepo{}
			svc := newSubscribeService(&fakeGithubClient{}, repo, ch)

			err := svc.Subscribe(context.Background(), SubscribeCommand{
				Email: validEmail,
				Repo:  tc.repo,
			})

			if !errors.Is(err, tc.wantErr) {
				t.Errorf("got %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestSubscribe_GitHubErrors(t *testing.T) {
	tests := []struct {
		name      string
		githubErr error
	}{
		{"repo not found", subscriptions.ErrRepoNotFound},
		{"unexpected error", errors.New("connection timeout")},
		{"rate limited", &subscriptions.RateLimitError{Service: "GitHub", RetryAfter: 30 * time.Second}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan mail.Message, 1)
			repo := &fakeSubscriptionRepo{}
			svc := newSubscribeService(&fakeGithubClient{validateRepoErr: tc.githubErr}, repo, ch)

			err := svc.Subscribe(context.Background(), SubscribeCommand{
				Email: validEmail,
				Repo:  validRepo,
			})

			if !errors.Is(err, tc.githubErr) {
				t.Errorf("got %v, want original github error %v", err, tc.githubErr)
			}
		})
	}
}

func TestSubscribe_TokenRepoErrors(t *testing.T) {
	tests := []struct {
		name         string
		tokenRepoErr error
	}{
		{"already subscribed", subscriptions.ErrAlreadySubscribed},
		{"unexpected db error", errors.New("connection reset by peer")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan mail.Message, 1)
			repo := &fakeSubscriptionRepo{subscribeErr: tc.tokenRepoErr}
			svc := newSubscribeService(&fakeGithubClient{}, repo, ch)

			err := svc.Subscribe(context.Background(), SubscribeCommand{
				Email: validEmail,
				Repo:  validRepo,
			})

			if !errors.Is(err, tc.tokenRepoErr) {
				t.Errorf("got %v, want %v", err, tc.tokenRepoErr)
			}
		})
	}
}

func TestSubscribe_EmailChannel(t *testing.T) {
	tests := []struct {
		name         string
		chanCap      int
		wantEnqueued bool
	}{
		{"channel has capacity", 1, true},
		{"channel full (unbuffered)", 0, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan mail.Message, tc.chanCap)
			repo := &fakeSubscriptionRepo{}
			svc := newSubscribeService(&fakeGithubClient{}, repo, ch)

			err := svc.Subscribe(context.Background(), SubscribeCommand{
				Email: validEmail,
				Repo:  validRepo,
			})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			enqueued := len(ch) == 1
			if enqueued != tc.wantEnqueued {
				t.Errorf("enqueued = %v, want %v", enqueued, tc.wantEnqueued)
			}
		})
	}
}
