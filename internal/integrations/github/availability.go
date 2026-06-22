package github

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

// DefaultAvailabilityCheckInterval is the cadence for polling GitHub operational state.
const DefaultAvailabilityCheckInterval = time.Minute

// AvailabilityChecker verifies GitHub availability and reports rate-limit state.
type AvailabilityChecker interface {
	CheckAvailabilityStatus(context.Context) (Availability, error)
}

// AvailabilityState stores the latest observed GitHub operational state.
type AvailabilityState struct {
	available          atomic.Int64
	rateLimitRemaining atomic.Int64
}

// NewAvailabilityState returns an unknown/unavailable initial GitHub state.
func NewAvailabilityState() *AvailabilityState {
	state := &AvailabilityState{}
	state.rateLimitRemaining.Store(-1)
	return state
}

// Set records the latest observed GitHub state.
func (s *AvailabilityState) Set(status Availability, available bool) {
	if available {
		s.available.Store(1)
	} else {
		s.available.Store(0)
	}
	s.rateLimitRemaining.Store(int64(status.Remaining))
}

// Available reports whether the latest GitHub availability check passed.
func (s *AvailabilityState) Available() bool {
	return s.available.Load() == 1
}

// RateLimitRemaining returns the latest observed GitHub core API budget.
func (s *AvailabilityState) RateLimitRemaining() int {
	return int(s.rateLimitRemaining.Load())
}

// StartAvailabilityMonitor refreshes GitHub operational state until ctx is cancelled.
func StartAvailabilityMonitor(
	ctx context.Context,
	checker AvailabilityChecker,
	state *AvailabilityState,
	log logger.Logger,
	interval time.Duration,
) {
	log.Info("github availability monitor started", "interval", interval)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("github availability monitor stopped")
			return
		case <-ticker.C:
			checkCtx, checkCancel := context.WithTimeout(ctx, 5*time.Second)
			UpdateAvailability(checkCtx, checker, state, log)
			checkCancel()
		}
	}
}

// UpdateAvailability performs one GitHub availability check and stores the result.
func UpdateAvailability(
	ctx context.Context,
	checker AvailabilityChecker,
	state *AvailabilityState,
	log logger.Logger,
) {
	status, err := checker.CheckAvailabilityStatus(ctx)
	if err != nil {
		state.Set(status, false)
		log.Error("github availability check failed", "err", err)
		return
	}
	state.Set(status, true)
}
