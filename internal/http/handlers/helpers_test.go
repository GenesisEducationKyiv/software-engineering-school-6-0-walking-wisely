package handlers

import (
	"context"
	"sync"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/domain"
)

// helperStream captures headers set via grpc.SetHeader.
// Kept in this file (package handlers) so it can coexist with
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
// isValidEmail
// ---------------------------------------------------------------------------

func TestIsValidEmail(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty string", "", false},
		{"no at-sign", "notanemail", false},
		{"multiple at-signs", "a@b@c.com", false},
		{"empty local part", "@domain.com", false},
		{"domain without dot", "user@domain", false},
		{"empty domain", "user@", false},
		{"valid", "user@example.com", true},
		{"subdomain", "user@sub.example.com", true},
		{"dot in local part", "first.last@example.com", true},
		{"plus in local part", "user+tag@example.com", true},
		// Quirk: implementation only checks for presence of a dot, not TLD validity.
		{"trailing dot in domain", "user@example.", true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isValidEmail(tc.input); got != tc.want {
				t.Errorf("isValidEmail(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// isValidToken
// ---------------------------------------------------------------------------

func TestIsValidToken(t *testing.T) {
	// 64-char valid lowercase hex string used as a base.
	const valid = "a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2"

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{"empty string", "", false},
		{"63 valid hex chars", valid[:63], false},
		{"65 valid hex chars", valid + "a", false},
		{"64 lowercase hex", valid, true},
		{"all zeros", "0000000000000000000000000000000000000000000000000000000000000000", true},
		{"all fs", "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff", true},
		{"uppercase hex char", "A" + valid[1:], false},
		{"non-hex letter g", "g" + valid[1:], false},
		{"space character", " " + valid[1:], false},
		{"hyphen character", "-" + valid[1:], false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := isValidToken(tc.input); got != tc.want {
				t.Errorf("isValidToken(%q) = %v, want %v", tc.input, got, tc.want)
			}
		})
	}
}

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

			err := handleRateLimitError(ctx, &domain.RateLimitError{
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
