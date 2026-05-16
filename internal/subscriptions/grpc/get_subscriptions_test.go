package subscriptiongrpc_test

import (
	"context"
	"errors"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

func strPtr(s string) *string { return &s }

// ---------------------------------------------------------------------------
// Email validation wiring — two cases only; exhaustive logic lives in app validation tests.
// ---------------------------------------------------------------------------

func TestGetSubscriptions_EmailValidation(t *testing.T) {
	repo := &fakeSubscriptionRepo{}
	svc := newService(&fakeGithubClient{}, repo, repo, nil)

	t.Run("invalid email rejected", func(t *testing.T) {
		_, err := svc.GetSubscriptions(context.Background(), &pb.GetSubscriptionsRequest{
			Email: "not-an-email",
		})
		if got := status.Code(err); got != codes.InvalidArgument {
			t.Errorf("got %v, want InvalidArgument", got)
		}
	})

	t.Run("valid email proceeds", func(t *testing.T) {
		_, err := svc.GetSubscriptions(context.Background(), &pb.GetSubscriptionsRequest{
			Email: validEmail,
		})
		if got := status.Code(err); got != codes.OK {
			t.Errorf("got %v, want OK", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Repository layer errors
// ---------------------------------------------------------------------------

func TestGetSubscriptions_RepoError(t *testing.T) {
	repo := &fakeSubscriptionRepo{
		listByEmailErr: errors.New("connection reset by peer"),
	}
	svc := newService(&fakeGithubClient{}, repo, repo, nil)

	_, err := svc.GetSubscriptions(context.Background(), &pb.GetSubscriptionsRequest{
		Email: validEmail,
	})
	if got := status.Code(err); got != codes.Internal {
		t.Errorf("got %v, want Internal", got)
	}
}

// ---------------------------------------------------------------------------
// Empty result
// ---------------------------------------------------------------------------

func TestGetSubscriptions_NoSubscriptions(t *testing.T) {
	repo := &fakeSubscriptionRepo{}
	svc := newService(&fakeGithubClient{}, repo, repo, nil)

	resp, err := svc.GetSubscriptions(context.Background(), &pb.GetSubscriptionsRequest{
		Email: validEmail,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Subscriptions) != 0 {
		t.Errorf("got %d subscriptions, want 0", len(resp.Subscriptions))
	}
}

// ---------------------------------------------------------------------------
// Response field mapping
// ---------------------------------------------------------------------------

func TestGetSubscriptions_ResponseMapping(t *testing.T) {
	subs := []subscriptions.Subscription{
		{Email: validEmail, Repo: "owner/alpha", Confirmed: true, LastSeenTag: strPtr("v1.2.3")},
		{Email: validEmail, Repo: "owner/beta", Confirmed: false, LastSeenTag: strPtr("v0.1.0")},
	}
	repo := &fakeSubscriptionRepo{listByEmailResult: subs}
	svc := newService(&fakeGithubClient{}, repo, repo, nil)

	resp, err := svc.GetSubscriptions(context.Background(), &pb.GetSubscriptionsRequest{
		Email: validEmail,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Subscriptions) != 2 {
		t.Fatalf("got %d subscriptions, want 2", len(resp.Subscriptions))
	}

	first := resp.Subscriptions[0]
	if first.Email != validEmail || first.Repo != "owner/alpha" || !first.Confirmed || first.LastSeenTag != "v1.2.3" {
		t.Errorf("first subscription mismatch: %+v", first)
	}

	second := resp.Subscriptions[1]
	if second.Confirmed {
		t.Errorf("expected Confirmed=false for second subscription, got true")
	}
	if second.LastSeenTag != "v0.1.0" {
		t.Errorf("second LastSeenTag = %q, want %q", second.LastSeenTag, "v0.1.0")
	}
}

func TestGetSubscriptions_NilLastSeenTag(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("handler panicked on nil LastSeenTag (subscriptions.go:33): %v", r)
		}
	}()

	subs := []subscriptions.Subscription{
		{Email: validEmail, Repo: validRepo, Confirmed: false, LastSeenTag: nil},
	}
	repo := &fakeSubscriptionRepo{listByEmailResult: subs}
	svc := newService(&fakeGithubClient{}, repo, repo, nil)

	resp, err := svc.GetSubscriptions(context.Background(), &pb.GetSubscriptionsRequest{
		Email: validEmail,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := resp.Subscriptions[0].LastSeenTag; got != "" {
		t.Errorf("LastSeenTag = %q, want empty string for nil pointer", got)
	}
}
