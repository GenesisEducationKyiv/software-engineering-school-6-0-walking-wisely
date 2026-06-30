package domain

import "errors"

var (
	ErrAlreadySubscribed = errors.New("email already subscribed to this repository")
	ErrTokenNotFound     = errors.New("token not found")
	ErrInvalidEmail      = errors.New("invalid email format")
	ErrInvalidRepo       = errors.New("invalid repo format, expected owner/repo")
	ErrInvalidToken      = errors.New("invalid token format")
)
