package subscriptiongrpc

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts"
)

// helperStream captures headers set via grpc.SetHeader.
// Kept in this file (package subscriptiongrpc) so it can coexist with
// the external-package fake in fakes_test.go without a name collision.
type helperStream struct {
	mu      sync.Mutex
	headers metadata.MD
}

func (s *helperStream) Method() string { return "" }
func (s *helperStream) SetHeader(md metadata.MD) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.headers = md
	return nil
}
func (s *helperStream) SendHeader(_ metadata.MD) error { return nil }
func (s *helperStream) SetTrailer(_ metadata.MD) error { return nil }

// ---------------------------------------------------------------------------
// handleRateLimitError
// ---------------------------------------------------------------------------

func TestHandleRateLimitError(t *testing.T) {
	tests := []struct {
		name       string
		retryAfter time.Duration
		wantHeader string // expected Retry-After header value
	}{
		{"whole seconds", 30 * time.Second, "30"},
		{"fractional seconds rounds up", 30*time.Second + 500*time.Millisecond, "31"},
		{"zero duration", 0, "0"},
		{"sub-second rounds up to one", 1 * time.Millisecond, "1"},
		{"exactly one second", 1 * time.Second, "1"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			stream := &helperStream{}
			ctx := grpc.NewContextWithServerTransportStream(context.Background(), stream)
			svc := NewSubscriptionService(&ServiceDeps{})

			err := svc.handleRateLimitError(ctx, &contracts.RateLimitError{
				Service:    "GitHub",
				RetryAfter: tc.retryAfter,
			})

			if got := status.Code(err); got != codes.Unavailable {
				t.Fatalf("got code %v, want Unavailable", got)
			}

			stream.mu.Lock()
			vals := stream.headers.Get("retry-after")
			stream.mu.Unlock()

			if len(vals) == 0 || vals[0] != tc.wantHeader {
				t.Errorf("Retry-After header = %v, want [%q]", vals, tc.wantHeader)
			}
		})
	}
}
