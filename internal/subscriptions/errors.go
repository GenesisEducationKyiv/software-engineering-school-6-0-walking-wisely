// Package domain contains the core business types, error sentinels, and token generation utilities.
package subscriptions

import (
	"errors"
	"fmt"
	"time"
)

var (
	// ErrAlreadySubscribed is returned when a confirmed subscription already exists for the email+repo pair.
	ErrAlreadySubscribed = errors.New("email already subscribed to this repository")

	// ErrTokenNotFound is returned when a confirm or unsubscribe token has no matching row.
	ErrTokenNotFound = errors.New("token not found")

	// ErrRepoNotFound is returned when GitHub reports the repository does not exist.
	ErrRepoNotFound = errors.New("repository not found on GitHub")
)

// RateLimitError is returned by external clients (GitHub, Resend) when the API
// responds with a rate-limit status. RetryAfter is parsed from the Retry-After
// response header so callers can surface an accurate wait time to the user.
type RateLimitError struct {
	Service    string
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s rate limited, retry after %s", e.Service, e.RetryAfter.Round(time.Second))
}
