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

// SubscribeAction describes the safe, non-PII outcome of a subscribe request.
type SubscribeAction string

const (
	SubscribeActionCreated               SubscribeAction = "created"
	SubscribeActionConfirmationRefreshed SubscribeAction = "confirmation_refreshed"
)

// SubscribeResult identifies the subscription row affected by a subscribe request.
type SubscribeResult struct {
	SubscriptionID string
	Action         SubscribeAction
}
