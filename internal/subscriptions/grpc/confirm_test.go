package subscriptiongrpc_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

// validToken is shared with unsubscribe_test.go (same package).

func TestConfirm_HappyPath(t *testing.T) {
	repo := &fakeSubscriptionRepo{confirmByTokenID: "sub-123"}
	svc := newService(&fakeGithubClient{}, repo, repo)

	resp, err := svc.ConfirmSubscription(context.Background(), &pb.ConfirmSubscriptionRequest{Token: validToken})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("response is nil")
	}
}

func TestConfirm_InvalidToken(t *testing.T) {
	// One representative bad token to exercise gRPC status mapping.
	// Exhaustive token-format cases live in the app package validation tests.
	repo := &fakeSubscriptionRepo{}
	svc := newService(&fakeGithubClient{}, repo, repo)

	_, err := svc.ConfirmSubscription(context.Background(), &pb.ConfirmSubscriptionRequest{Token: "not-a-valid-token"}) // #nosec G101 -- test-only invalid token.

	if got := status.Code(err); got != codes.InvalidArgument {
		t.Errorf("got %v, want InvalidArgument", got)
	}
}

func TestConfirm_RepoErrors(t *testing.T) {
	tests := []struct {
		name     string
		repoErr  error
		wantCode codes.Code
	}{
		{"token not found", subscriptionsdomain.ErrTokenNotFound, codes.NotFound},
		{"token not found wrapped", fmt.Errorf("db: %w", subscriptionsdomain.ErrTokenNotFound), codes.NotFound},
		{"unexpected db error", errors.New("connection refused"), codes.Internal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			repo := &fakeSubscriptionRepo{confirmByTokenErr: tc.repoErr}
			svc := newService(&fakeGithubClient{}, repo, repo)

			_, err := svc.ConfirmSubscription(context.Background(), &pb.ConfirmSubscriptionRequest{Token: validToken})

			if got := status.Code(err); got != tc.wantCode {
				t.Errorf("got %v, want %v", got, tc.wantCode)
			}
		})
	}
}
