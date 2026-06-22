package github

import (
	"context"
	"errors"
	"testing"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

type availabilityTestChecker struct {
	status Availability
	err    error
}

func (c availabilityTestChecker) CheckAvailabilityStatus(context.Context) (Availability, error) {
	return c.status, c.err
}

func TestAvailabilityStateDefaultsToUnknownUnavailable(t *testing.T) {
	t.Parallel()

	state := NewAvailabilityState()

	if state.Available() {
		t.Fatalf("Available() = true, want false")
	}
	if state.RateLimitRemaining() != -1 {
		t.Fatalf("RateLimitRemaining() = %d, want -1", state.RateLimitRemaining())
	}
}

func TestUpdateAvailabilityRecordsSuccess(t *testing.T) {
	t.Parallel()

	state := NewAvailabilityState()
	checker := availabilityTestChecker{status: Availability{Authenticated: true, Remaining: 123}}

	UpdateAvailability(context.Background(), checker, state, logger.NoopLogger{})

	if !state.Available() {
		t.Fatalf("Available() = false, want true")
	}
	if state.RateLimitRemaining() != 123 {
		t.Fatalf("RateLimitRemaining() = %d, want 123", state.RateLimitRemaining())
	}
}

func TestUpdateAvailabilityRecordsFailureWithObservedState(t *testing.T) {
	t.Parallel()

	state := NewAvailabilityState()
	checker := availabilityTestChecker{
		status: Availability{Authenticated: true, Remaining: 0},
		err:    errors.New("rate limited"),
	}

	UpdateAvailability(context.Background(), checker, state, logger.NoopLogger{})

	if state.Available() {
		t.Fatalf("Available() = true, want false")
	}
	if state.RateLimitRemaining() != 0 {
		t.Fatalf("RateLimitRemaining() = %d, want 0", state.RateLimitRemaining())
	}
}
