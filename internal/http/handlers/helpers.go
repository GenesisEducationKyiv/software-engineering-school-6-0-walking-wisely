package handlers

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/domain"
)

func handleRateLimitError(ctx context.Context, rle *domain.RateLimitError) error {
	secs := int(math.Ceil(rle.RetryAfter.Seconds()))

	header := metadata.Pairs("Retry-After", fmt.Sprintf("%d", secs))
	if err := grpc.SetHeader(ctx, header); err != nil {
		slog.ErrorContext(ctx, "failed to set gRPC retry header", "error", err)
	}

	msg := fmt.Sprintf("service temporarily unavailable, retry after %ds", secs)

	return status.Error(codes.Unavailable, msg)
}

// isValidEmail s a lightweight sanity check - not RFC 5321 complete but
// sufficient to reject obvious garbage before hitting the database.
func isValidEmail(email string) bool {
	parts := strings.Split(email, "@")
	return len(parts) == 2 && parts[0] != "" && strings.Contains(parts[1], ".")
}

// isValidToken checks that the string is a 64-character lowercase hex string,
// which is the output format of our HMAC-SHA256 token generator.
func isValidToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	for _, c := range token {
		//nolint:staticcheck // De Morgan's law makes this less readable here
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
