package subscriptionapp

import (
	"context"
	"errors"
	"strings"
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
	calls        int
	ctx          context.Context
	email        string
	repo         string
	confirmToken string
	unsubToken   string
}

func (f *fakeSubscriptionRepo) Subscribe(ctx context.Context, email, repo, confirmToken, unsubToken string) error {
	f.calls++
	f.ctx = ctx
	f.email = email
	f.repo = repo
	f.confirmToken = confirmToken
	f.unsubToken = unsubToken
	return f.subscribeErr
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

func TestSubscribe(t *testing.T) {
	ch := make(chan mail.Message, 1)
	repo := &fakeSubscriptionRepo{}
	gh := &fakeGithubClient{}
	svc := newSubscribeService(gh, repo, ch)
	ctx := context.WithValue(context.Background(), testContextKey{}, "request-123")

	err := svc.Subscribe(ctx, SubscribeCommand{
		Email: "  User@Example.COM  ",
		Repo:  "  owner/repo  ",
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if gh.calls != 1 {
		t.Fatalf("github calls = %d, want 1", gh.calls)
	}
	if gh.ctx != ctx {
		t.Errorf("github context was not passed through")
	}
	if gh.repo != validRepo {
		t.Errorf("github repo = %q, want %q", gh.repo, validRepo)
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

	if len(ch) != 1 {
		t.Fatalf("queued messages = %d, want 1", len(ch))
	}
	msg := <-ch
	if msg.To != validEmail {
		t.Errorf("message To = %q, want %q", msg.To, validEmail)
	}
	for _, want := range []string{
		validRepo,
		"http://localhost/api/confirm/" + repo.confirmToken,
		"http://localhost/api/unsubscribe/" + repo.unsubToken,
	} {
		if !strings.Contains(msg.HTML, want) {
			t.Errorf("message HTML does not contain %q: %s", want, msg.HTML)
		}
	}
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
			gh := &fakeGithubClient{}
			svc := newSubscribeService(gh, repo, ch)

			err := svc.Subscribe(context.Background(), SubscribeCommand{
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
				if len(ch) != 0 {
					t.Errorf("queued messages = %d, want 0", len(ch))
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
			gh := &fakeGithubClient{}
			svc := newSubscribeService(gh, repo, ch)

			err := svc.Subscribe(context.Background(), SubscribeCommand{
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
				if len(ch) != 0 {
					t.Errorf("queued messages = %d, want 0", len(ch))
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
		{"repo not found", subscriptions.ErrRepoNotFound},
		{"unexpected error", errors.New("connection timeout")},
		{"rate limited", &subscriptions.RateLimitError{Service: "GitHub", RetryAfter: 30 * time.Second}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan mail.Message, 1)
			repo := &fakeSubscriptionRepo{}
			gh := &fakeGithubClient{validateRepoErr: tc.githubErr}
			svc := newSubscribeService(gh, repo, ch)

			err := svc.Subscribe(context.Background(), SubscribeCommand{
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
			if len(ch) != 0 {
				t.Errorf("queued messages = %d, want 0", len(ch))
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
			gh := &fakeGithubClient{}
			svc := newSubscribeService(gh, repo, ch)

			err := svc.Subscribe(context.Background(), SubscribeCommand{
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
			if len(ch) != 0 {
				t.Errorf("queued messages = %d, want 0", len(ch))
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
