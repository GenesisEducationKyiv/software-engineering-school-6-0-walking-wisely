package subscriptions

import (
	"time"
)

// Subscription represents a single email → repo notification subscription.
type Subscription struct {
	ID               string
	Email            string
	Repo             string
	Confirmed        bool
	ConfirmToken     string
	UnsubscribeToken string
	LastSeenTag      *string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}
