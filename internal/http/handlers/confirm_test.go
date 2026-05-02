package handlers_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/domain"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// validToken is shared with unsubscribe_test.go (same package).

func TestConfirm_HappyPath(t *testing.T) {
	svc := newService(&fakeGithubClient{}, &fakeSubRepo{confirmByTokenID: "sub-123"}, nil)

	resp, err := svc.ConfirmSubscription(context.Background(), &pb.ConfirmSubscriptionRequest{Token: validToken})

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp == nil {
		t.Fatal("response is nil")
	}
}

func TestConfirm_InvalidToken(t *testing.T) {
	// One representative bad token to exercise the isValidToken branch.
	// Exhaustive token-format cases live in TestIsValidToken (helpers_test.go).
	svc := newService(&fakeGithubClient{}, &fakeSubRepo{}, nil)

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
		{"token not found", domain.ErrTokenNotFound, codes.NotFound},
		{"token not found wrapped", fmt.Errorf("db: %w", domain.ErrTokenNotFound), codes.NotFound},
		{"unexpected db error", errors.New("connection refused"), codes.Internal},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			svc := newService(&fakeGithubClient{}, &fakeSubRepo{confirmByTokenErr: tc.repoErr}, nil)

			_, err := svc.ConfirmSubscription(context.Background(), &pb.ConfirmSubscriptionRequest{Token: validToken})

			if got := status.Code(err); got != tc.wantCode {
				t.Errorf("got %v, want %v", got, tc.wantCode)
			}
		})
	}
}
