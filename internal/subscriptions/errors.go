package subscriptions

import subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"

var (
	ErrAlreadySubscribed = subscriptionsdomain.ErrAlreadySubscribed
	ErrTokenNotFound     = subscriptionsdomain.ErrTokenNotFound
	ErrRepoNotFound      = subscriptionsdomain.ErrRepoNotFound
	ErrInvalidEmail      = subscriptionsdomain.ErrInvalidEmail
	ErrInvalidRepo       = subscriptionsdomain.ErrInvalidRepo
	ErrInvalidToken      = subscriptionsdomain.ErrInvalidToken
)

type RateLimitError = subscriptionsdomain.RateLimitError
