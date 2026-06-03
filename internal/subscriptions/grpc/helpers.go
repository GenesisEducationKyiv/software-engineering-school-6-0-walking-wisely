package subscriptiongrpc

import (
	"context"
	"fmt"
	"math"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

func (s *SubscriptionService) handleRateLimitError(ctx context.Context, rle *subscriptions.RateLimitError) error {
	secs := int(math.Ceil(rle.RetryAfter.Seconds()))
	s.log.Warn("rate limited dependency",
		"service", rle.Service,
		"retry_after", rle.RetryAfter)

	header := metadata.Pairs("Retry-After", fmt.Sprintf("%d", secs))
	if err := grpc.SetHeader(ctx, header); err != nil {
		s.log.ErrorContext(ctx, "failed to set gRPC retry header", "error", err)
	}

	msg := fmt.Sprintf("service temporarily unavailable, retry after %ds", secs)

	return status.Error(codes.Unavailable, msg)
}
