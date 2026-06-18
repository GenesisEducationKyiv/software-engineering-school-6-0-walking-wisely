package subscriptions

import subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"

var (
	ErrAlreadySubscribed = subscriptionsdomain.ErrAlreadySubscribed
	ErrTokenNotFound     = subscriptionsdomain.ErrTokenNotFound
	ErrInvalidEmail      = subscriptionsdomain.ErrInvalidEmail
	ErrInvalidRepo       = subscriptionsdomain.ErrInvalidRepo
	ErrInvalidToken      = subscriptionsdomain.ErrInvalidToken
)
