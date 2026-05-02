package handlers_test

import (
	"context"
	"errors"
	"testing"
	"time"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/domain"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/http/handlers"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

const (
	validEmail = "user@example.com"
	validRepo  = "owner/repo"
)

func newService(gh handlers.GithubClient, repo handlers.SubRepo, ch chan domain.EmailMessage) *handlers.SubscriptionService {
	return handlers.NewSubscriptionService(handlers.ServiceDeps{
		SubRepo:        repo,
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
		{"empty local part", "@domain.com", codes.InvalidArgument},
		{"domain without dot", "user@domain", codes.InvalidArgument},
		{"empty domain", "user@", codes.InvalidArgument},
		{"valid", validEmail, codes.OK},
		{"trims whitespace", "  user@example.com  ", codes.OK},
		{"lowercases uppercase", "User@Example.COM", codes.OK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan domain.EmailMessage, 1)
			svc := newService(&fakeGithubClient{}, &fakeSubRepo{}, ch)

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
			ch := make(chan domain.EmailMessage, 1)
			svc := newService(&fakeGithubClient{}, &fakeSubRepo{}, ch)

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
		{"repo not found", domain.ErrRepoNotFound, codes.NotFound},
		{"unexpected error", errors.New("connection timeout"), codes.Internal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan domain.EmailMessage, 1)
			svc := newService(&fakeGithubClient{validateRepoErr: tc.githubErr}, &fakeSubRepo{}, ch)

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
	ch := make(chan domain.EmailMessage, 1)
	svc := newService(
		&fakeGithubClient{validateRepoErr: &domain.RateLimitError{Service: "GitHub", RetryAfter: 30 * time.Second}},
		&fakeSubRepo{},
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

func TestSubscribe_SubRepoErrors(t *testing.T) {
	tests := []struct {
		name       string
		subRepoErr error
		wantCode   codes.Code
	}{
		{"already subscribed", domain.ErrAlreadySubscribed, codes.AlreadyExists},
		{"unexpected db error", errors.New("connection reset by peer"), codes.Internal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan domain.EmailMessage, 1)
			svc := newService(&fakeGithubClient{}, &fakeSubRepo{subscribeErr: tc.subRepoErr}, ch)

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
			ch := make(chan domain.EmailMessage, tc.chanCap)
			svc := newService(&fakeGithubClient{}, &fakeSubRepo{}, ch)

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
