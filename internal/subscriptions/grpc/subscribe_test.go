package subscriptiongrpc_test

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
	subscriptionapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app"
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
	bus := events.NewBus()
	bus.Subscribe(subscriptionapp.SubscriptionRequested{}.EventName(), func(_ context.Context, event events.Event) error {
		requested, ok := event.(subscriptionapp.SubscriptionRequested)
		if !ok {
			return fmt.Errorf("event type = %T, want %T", event, subscriptionapp.SubscriptionRequested{})
		}
		select {
		case ch <- mail.Message{To: requested.Email}:
		default:
		}
		return nil
	})

	return subscriptiongrpc.NewSubscriptionService(&subscriptiongrpc.ServiceDeps{
		TokenRepo:      tokenRepo,
		ReadRepo:       readRepo,
		TxManager:      fakeTxManager{},
		Github:         gh,
		Publisher:      bus,
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
		{"repo not found", validEmail, validRepo, subscriptions.ErrRepoNotFound, nil, codes.NotFound},
		{"already subscribed", validEmail, validRepo, nil, subscriptions.ErrAlreadySubscribed, codes.AlreadyExists},
		{"unexpected github error", validEmail, validRepo, errors.New("connection timeout"), nil, codes.Internal},
		{"unexpected db error", validEmail, validRepo, nil, errors.New("connection reset by peer"), codes.Internal},
		{"success", validEmail, validRepo, nil, nil, codes.OK},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ch := make(chan mail.Message, 1)
			repo := &fakeSubscriptionRepo{subscribeErr: tc.db}
			svc := newService(&fakeGithubClient{validateRepoErr: tc.github}, repo, repo, ch)

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
