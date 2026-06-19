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
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts"
	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
	subscriptiongrpc "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/grpc"
)

const (
	validEmail = "user@example.com"
	validRepo  = "owner/repo"
)

// fakeOrchestrator is a no-op saga orchestrator used in unit tests.
type fakeOrchestrator struct{}

func (fakeOrchestrator) EnqueueWithinTx(_ context.Context, _, _, _, _, _ string) error { return nil }

func newService(
	gh subscriptiongrpc.GithubRepoValidator,
	tokenRepo subscriptiongrpc.SubscriptionTokenWorkflowRepo,
	readRepo subscriptiongrpc.SubscriptionReadRepo,
) *subscriptiongrpc.SubscriptionService {
	return subscriptiongrpc.NewSubscriptionService(&subscriptiongrpc.ServiceDeps{
		TokenRepo:      tokenRepo,
		ReadRepo:       readRepo,
		TxManager:      fakeTxManager{},
		Github:         gh,
		Orchestrator:   fakeOrchestrator{},
		EmailSecretKey: "test-secret",
	})
}

func TestSubscribe_StatusMapping(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		repo     string
		github   error
		db       error
		wantCode codes.Code
	}{
		{"invalid email", "notanemail", validRepo, nil, nil, codes.InvalidArgument},
		{"invalid repo", validEmail, "owneronly", nil, nil, codes.InvalidArgument},
		{"repo not found", validEmail, validRepo, contracts.ErrRepoNotFound, nil, codes.NotFound},
		{"already subscribed", validEmail, validRepo, nil, subscriptionsdomain.ErrAlreadySubscribed, codes.AlreadyExists},
		{"unexpected github error", validEmail, validRepo, errors.New("connection timeout"), nil, codes.Internal},
		{"unexpected db error", validEmail, validRepo, nil, errors.New("connection reset by peer"), codes.Internal},
		{"success", validEmail, validRepo, nil, nil, codes.OK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeSubscriptionRepo{subscribeErr: tc.db}
			svc := newService(&fakeGithubClient{validateRepoErr: tc.github}, repo, repo)

			_, err := svc.Subscribe(context.Background(), &pb.SubscribeRequest{
				Email: tc.email,
				Repo:  tc.repo,
			})

			if got := status.Code(err); got != tc.wantCode {
				t.Errorf("got %v, want %v", got, tc.wantCode)
			}
		})
	}
}

func TestSubscribe_RateLimit(t *testing.T) {
	repo := &fakeSubscriptionRepo{}
	svc := newService(
		&fakeGithubClient{validateRepoErr: &contracts.RateLimitError{Service: "GitHub", RetryAfter: 30 * time.Second}},
		repo,
		repo,
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
