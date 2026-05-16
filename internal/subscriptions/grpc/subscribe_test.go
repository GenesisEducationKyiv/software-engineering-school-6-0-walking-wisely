package subscriptiongrpc_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
	subscriptiongrpc "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/grpc"
)

const (
	validEmail = "user@example.com"
	validRepo  = "owner/repo"
)

func newService(
	gh subscriptiongrpc.GithubRepoValidator,
	tokenRepo subscriptiongrpc.SubscriptionTokenWorkflowRepo,
	readRepo subscriptiongrpc.SubscriptionReadRepo,
	ch chan mail.Message,
) *subscriptiongrpc.SubscriptionService {
	return subscriptiongrpc.NewSubscriptionService(subscriptiongrpc.ServiceDeps{
		TokenRepo:      tokenRepo,
		ReadRepo:       readRepo,
		Github:         gh,
		EmailChan:      ch,
		EmailSecretKey: "test-secret",
		BaseURL:        "http://localhost",
	})
}

func TestSubscribe_EmailValidation(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		wantCode codes.Code
	}{
		{"empty string", "", codes.InvalidArgument},
		{"no at-sign", "notanemail", codes.InvalidArgument},
		{"multiple at-signs", "a@b@c.com", codes.InvalidArgument},
		{"empty local part", "@subscriptions.com", codes.InvalidArgument},
		{"domain without dot", "user@domain", codes.InvalidArgument},
		{"empty domain", "user@", codes.InvalidArgument},
		{"valid", validEmail, codes.OK},
		{"trims whitespace", "  user@example.com  ", codes.OK},
		{"lowercases uppercase", "User@Example.COM", codes.OK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan mail.Message, 1)
			repo := &fakeSubscriptionRepo{}
			svc := newService(&fakeGithubClient{}, repo, repo, ch)

			_, err := svc.Subscribe(context.Background(), &pb.SubscribeRequest{
				Email: tc.email,
				Repo:  validRepo,
			})

			if got := status.Code(err); got != tc.wantCode {
				t.Errorf("got %v, want %v", got, tc.wantCode)
			}
		})
	}
}

func TestSubscribe_RepoValidation(t *testing.T) {
	tests := []struct {
		name     string
		repo     string
		wantCode codes.Code
	}{
		{"empty string", "", codes.InvalidArgument},
		{"no slash", "owneronly", codes.InvalidArgument},
		{"slash only", "/", codes.InvalidArgument},
		{"empty owner", "/repo", codes.InvalidArgument},
		{"empty name", "owner/", codes.InvalidArgument},
		{"space in name", "owner/repo name", codes.InvalidArgument},
		{"too many slashes", "owner/repo/extra", codes.InvalidArgument},
		{"trims whitespace", "  owner/repo  ", codes.OK},
		{"allows dots hyphens underscores", "my.org/my-repo_v2", codes.OK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan mail.Message, 1)
			repo := &fakeSubscriptionRepo{}
			svc := newService(&fakeGithubClient{}, repo, repo, ch)

			_, err := svc.Subscribe(context.Background(), &pb.SubscribeRequest{
				Email: validEmail,
				Repo:  tc.repo,
			})

			if got := status.Code(err); got != tc.wantCode {
				t.Errorf("got %v, want %v", got, tc.wantCode)
			}
		})
	}
}

func TestSubscribe_GitHubErrors(t *testing.T) {
	tests := []struct {
		name      string
		githubErr error
		wantCode  codes.Code
	}{
		{"repo not found", subscriptions.ErrRepoNotFound, codes.NotFound},
		{"unexpected error", errors.New("connection timeout"), codes.Internal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan mail.Message, 1)
			repo := &fakeSubscriptionRepo{}
			svc := newService(&fakeGithubClient{validateRepoErr: tc.githubErr}, repo, repo, ch)

			_, err := svc.Subscribe(context.Background(), &pb.SubscribeRequest{
				Email: validEmail,
				Repo:  validRepo,
			})

			if got := status.Code(err); got != tc.wantCode {
				t.Errorf("got %v, want %v", got, tc.wantCode)
			}
		})
	}
}

func TestSubscribe_RateLimit(t *testing.T) {
	ch := make(chan mail.Message, 1)
	repo := &fakeSubscriptionRepo{}
	svc := newService(
		&fakeGithubClient{validateRepoErr: &subscriptions.RateLimitError{Service: "GitHub", RetryAfter: 30 * time.Second}},
		repo,
		repo,
		ch,
	)

	stream := &fakeServerStream{}
	ctx := grpc.NewContextWithServerTransportStream(context.Background(), stream)

	_, err := svc.Subscribe(ctx, &pb.SubscribeRequest{Email: validEmail, Repo: validRepo})

	if got := status.Code(err); got != codes.Unavailable {
		t.Fatalf("got code %v, want Unavailable", got)
	}

	stream.mu.Lock()
	retryAfter := stream.headers.Get("retry-after")
	stream.mu.Unlock()

	if len(retryAfter) == 0 || retryAfter[0] != "30" {
		t.Errorf("Retry-After header = %v, want [\"30\"]", retryAfter)
	}
}

func TestSubscribe_TokenRepoErrors(t *testing.T) {
	tests := []struct {
		name         string
		tokenRepoErr error
		wantCode     codes.Code
	}{
		{"already subscribed", subscriptions.ErrAlreadySubscribed, codes.AlreadyExists},
		{"unexpected db error", errors.New("connection reset by peer"), codes.Internal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan mail.Message, 1)
			repo := &fakeSubscriptionRepo{subscribeErr: tc.tokenRepoErr}
			svc := newService(&fakeGithubClient{}, repo, repo, ch)

			_, err := svc.Subscribe(context.Background(), &pb.SubscribeRequest{
				Email: validEmail,
				Repo:  validRepo,
			})

			if got := status.Code(err); got != tc.wantCode {
				t.Errorf("got %v, want %v", got, tc.wantCode)
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
			svc := newService(&fakeGithubClient{}, repo, repo, ch)

			_, err := svc.Subscribe(context.Background(), &pb.SubscribeRequest{
				Email: validEmail,
				Repo:  validRepo,
			})

			if got := status.Code(err); got != codes.OK {
				t.Fatalf("got code %v, want OK", got)
			}

			enqueued := len(ch) == 1
			if enqueued != tc.wantEnqueued {
				t.Errorf("enqueued = %v, want %v", enqueued, tc.wantEnqueued)
			}
		})
	}
}
