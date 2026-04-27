package handlers

import (
	"context"
	"fmt"
	"math"
	"strings"

	"github.com/walking-wisely/genesis2026-github-release-api/internal/domain"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

func handleRateLimitError(ctx context.Context, rle *domain.RateLimitError) error {
	secs := int(math.Ceil(rle.RetryAfter.Seconds()))

	header := metadata.Pairs("Retry-After", fmt.Sprintf("%d", secs))
	grpc.SetHeader(ctx, header)

	msg := fmt.Sprintf("service temporarily unavailable, retry after %ds", secs)

	return status.Error(codes.Unavailable, msg)
}

// isValidEmail s a lightweight sanity check - not RFC 5321 complete but
// sufficient to reject obvious garbage before hitting the database.
func isValidEmail(email string) bool {
	parts := strings.Split(email, "@")
	return len(parts) == 2 && len(parts[0]) > 0 && strings.Contains(parts[1], ".")
}

// isValidToken checks that the string is a 64-character lowercase hex string,
// which is the output format of our HMAC-SHA256 token generator.
func isValidToken(token string) bool {
	if len(token) != 64 {
		return false
	}
	for _, c := range token {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
