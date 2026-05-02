package handlers_test

import (
	"context"
	"sync"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/domain"
	"google.golang.org/grpc/metadata"
)

// fakeSubRepo implements SubRepo with configurable return values per method.
type fakeSubRepo struct {
	subscribeErr          error
	confirmByTokenID      string
	confirmByTokenErr     error
	unsubscribeByTokenID  string
	unsubscribeByTokenErr error
	listByEmailResult     []domain.Subscription
	listByEmailErr        error
}

func (f *fakeSubRepo) Subscribe(_ context.Context, _, _, _, _ string) error {
	return f.subscribeErr
}
func (f *fakeSubRepo) ConfirmByToken(_ context.Context, _ string) (string, error) {
	return f.confirmByTokenID, f.confirmByTokenErr
}
func (f *fakeSubRepo) UnsubscribeByToken(_ context.Context, _ string) (string, error) {
	return f.unsubscribeByTokenID, f.unsubscribeByTokenErr
}
func (f *fakeSubRepo) ListByEmail(_ context.Context, _ string) ([]domain.Subscription, error) {
	return f.listByEmailResult, f.listByEmailErr
}
func (f *fakeSubRepo) ListDistinctConfirmedRepos(_ context.Context) ([]string, error) {
	return nil, nil
}
func (f *fakeSubRepo) ListConfirmedSubscribersForRepo(_ context.Context, _ string) ([]domain.Subscription, error) {
	return nil, nil
}
func (f *fakeSubRepo) UpdateLastSeenTag(_ context.Context, _, _ string) error { return nil }

// fakeGithubClient implements GithubClient with a configurable error.
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
func (s *fakeServerStream) SendHeader(md metadata.MD) error { return nil }
func (s *fakeServerStream) SetTrailer(_ metadata.MD) error  { return nil }
