package domain

import (
	"errors"
	"fmt"
	"time"
)

var (
	ErrAlreadySubscribed = errors.New("email already subscribed to this repository")
	ErrTokenNotFound     = errors.New("token not found")
	ErrRepoNotFound      = errors.New("repository not found on GitHub")
	ErrInvalidEmail      = errors.New("invalid email format")
	ErrInvalidRepo       = errors.New("invalid repo format, expected owner/repo")
	ErrInvalidToken      = errors.New("invalid token format")
)

// RateLimitError is returned by external clients when the dependency is rate limited.
type RateLimitError struct {
	Service    string
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s rate limited, retry after %s", e.Service, e.RetryAfter.Round(time.Second))
}
