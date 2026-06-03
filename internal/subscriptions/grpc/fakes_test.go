package subscriptiongrpc_test

import (
	"context"
	"sync"

	"google.golang.org/grpc/metadata"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

// fakeSubscriptionRepo implements the gRPC-facing subscription repository interfaces
// with configurable return values per method.
type fakeSubscriptionRepo struct {
	subscribeErr          error
	confirmByTokenID      string
	confirmByTokenErr     error
	unsubscribeByTokenID  string
	unsubscribeByTokenErr error
	listByEmailResult     []subscriptions.Subscription
	listByEmailErr        error
}

func (f *fakeSubscriptionRepo) Subscribe(_ context.Context, _, _, _, _ string) (subscriptions.SubscribeResult, error) {
	return subscriptions.SubscribeResult{
		SubscriptionID: "sub-1",
		Action:         subscriptions.SubscribeActionCreated,
	}, f.subscribeErr
}

func (f *fakeSubscriptionRepo) ConfirmByToken(_ context.Context, _ string) (string, error) {
	return f.confirmByTokenID, f.confirmByTokenErr
}

func (f *fakeSubscriptionRepo) UnsubscribeByToken(_ context.Context, _ string) (string, error) {
	return f.unsubscribeByTokenID, f.unsubscribeByTokenErr
}

func (f *fakeSubscriptionRepo) ListByEmail(_ context.Context, _ string) ([]subscriptions.Subscription, error) {
	return f.listByEmailResult, f.listByEmailErr
}

// fakeGithubClient implements RepositoryValidator with a configurable error.
type fakeGithubClient struct {
	validateRepoErr error
}

func (f *fakeGithubClient) ValidateRepo(_ context.Context, _ string) error {
	return f.validateRepoErr
}

// fakeServerStream implements grpc.ServerTransportStream and captures
// any headers set via grpc.SetHeader so tests can assert on them.
type fakeServerStream struct {
	mu      sync.Mutex
	headers metadata.MD
}

func (s *fakeServerStream) Method() string { return "" }
func (s *fakeServerStream) SetHeader(md metadata.MD) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.headers = md
	return nil
}
func (s *fakeServerStream) SendHeader(_ metadata.MD) error { return nil }
func (s *fakeServerStream) SetTrailer(_ metadata.MD) error { return nil }
