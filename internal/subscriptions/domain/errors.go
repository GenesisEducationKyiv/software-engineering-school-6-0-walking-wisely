package domain

import (
	"errors"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts"
)

var (
	ErrAlreadySubscribed = errors.New("email already subscribed to this repository")
	ErrTokenNotFound     = errors.New("token not found")
	ErrRepoNotFound      = contracts.ErrRepoNotFound
	ErrInvalidEmail      = errors.New("invalid email format")
	ErrInvalidRepo       = errors.New("invalid repo format, expected owner/repo")
	ErrInvalidToken      = errors.New("invalid token format")
)

type RateLimitError = contracts.RateLimitError
