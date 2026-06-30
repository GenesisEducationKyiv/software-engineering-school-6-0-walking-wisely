package subscriptiongrpc_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

// validToken is a well-formed 64-character lowercase hex string that passes
// token validation. The specific value is arbitrary; token-format edge cases are
// covered exhaustively in the app package validation tests.
const validToken = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

// ---------------------------------------------------------------------------
// Happy path
// ---------------------------------------------------------------------------

func TestUnsubscribe_Success(t *testing.T) {
	repo := &fakeSubscriptionRepo{unsubscribeByTokenID: "sub-42"}
	svc := newService(&fakeGithubClient{}, repo, repo)

	resp, err := svc.Unsubscribe(context.Background(), &pb.UnsubscribeRequest{Token: validToken})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("expected non-nil response")
	}
}

// ---------------------------------------------------------------------------
// Failure paths
// ---------------------------------------------------------------------------

func TestUnsubscribe_InvalidToken(t *testing.T) {
	// One representative bad token to exercise gRPC status mapping.
	// Exhaustive token-format cases live in the app package validation tests.
	repo := &fakeSubscriptionRepo{}
	svc := newService(&fakeGithubClient{}, repo, repo)

	_, err := svc.Unsubscribe(context.Background(), &pb.UnsubscribeRequest{Token: ""})

	if got := status.Code(err); got != codes.InvalidArgument {
		t.Errorf("got %v, want InvalidArgument", got)
	}
}

func TestUnsubscribe_RepoErrors(t *testing.T) {
	tests := []struct {
		name     string
		repoErr  error
		wantCode codes.Code
	}{
		// Exact sentinel → NotFound.
		{"token not found", subscriptionsdomain.ErrTokenNotFound, codes.NotFound},
		// Wrapped sentinel must still resolve via errors.Is → NotFound.
		{"wrapped token not found", fmt.Errorf("db layer: %w", subscriptionsdomain.ErrTokenNotFound), codes.NotFound},
		// Any other error → Internal.
		{"unexpected db error", errors.New("connection reset by peer"), codes.Internal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeSubscriptionRepo{unsubscribeByTokenErr: tc.repoErr}
			svc := newService(&fakeGithubClient{}, repo, repo)

			_, err := svc.Unsubscribe(context.Background(), &pb.UnsubscribeRequest{Token: validToken})

			if got := status.Code(err); got != tc.wantCode {
				t.Errorf("got %v, want %v", got, tc.wantCode)
			}
		})
	}
}

func TestUnsubscribe_InternalErrorDoesNotLeakDetail(t *testing.T) {
	const internalMsg = "pq: deadlock detected on table subscriptions"
	repo := &fakeSubscriptionRepo{unsubscribeByTokenErr: errors.New(internalMsg)}
	svc := newService(&fakeGithubClient{}, repo, repo)

	_, err := svc.Unsubscribe(context.Background(), &pb.UnsubscribeRequest{Token: validToken})

	s, _ := status.FromError(err)
	if strings.Contains(s.Message(), "pq:") || strings.Contains(s.Message(), "deadlock") {
		t.Errorf("error message leaks internal detail: %q", s.Message())
	}
}
