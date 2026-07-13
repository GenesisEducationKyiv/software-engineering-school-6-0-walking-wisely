//go:build bench

package notificationgrpc

import (
	"context"
	"time"

	notificationv1 "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/notification/v1"
)

// SlowServer wraps Server with a fixed synthetic service-time injected before
// each SendConfirmation call. Compiled only with -tags bench — never in a
// production binary.
type SlowServer struct {
	*Server
	delay time.Duration
}

// NewSlowServer returns a SlowServer that sleeps for delay before delegating to
// the real handler. Use this in cmd/notifications-bench to model a
// slow/recovering consumer without shipping the cost in prod.
func NewSlowServer(inner *Server, delay time.Duration) *SlowServer {
	return &SlowServer{Server: inner, delay: delay}
}

func (s *SlowServer) SendConfirmation(ctx context.Context, req *notificationv1.SendConfirmationRequest) (*notificationv1.Ack, error) {
	time.Sleep(s.delay)
	return s.Server.SendConfirmation(ctx, req)
}
