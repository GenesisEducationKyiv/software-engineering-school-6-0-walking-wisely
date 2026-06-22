package contracts

import (
	"errors"
	"fmt"
	"time"
)

var ErrRepoNotFound = errors.New("repository not found on GitHub")

// RateLimitError is returned when a dependency asks callers to retry later.
type RateLimitError struct {
	Service    string
	RetryAfter time.Duration
}

func (e *RateLimitError) Error() string {
	return fmt.Sprintf("%s rate limited, retry after %s", e.Service, e.RetryAfter.Round(time.Second))
}
