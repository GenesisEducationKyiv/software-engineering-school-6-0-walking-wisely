package subscriptionapp

import (
	"time"

	"github.com/google/uuid"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
)

// SubscriptionRequested is emitted after a watch request is persisted.
type SubscriptionRequested struct {
	events.Metadata
	SubscriptionID string
	Email          string
	Repo           string
	ConfirmToken   string
	UnsubToken     string
}

func (SubscriptionRequested) EventName() string {
	return "subscriptions.subscription_requested"
}

func (SubscriptionRequested) AggregateType() string {
	return "subscription"
}

// Keep a value receiver so decoded events can still be asserted as SubscriptionRequested.
//
//nolint:gocritic // Durable event decoding intentionally preserves value event types.
func (e SubscriptionRequested) AggregateID() string {
	return e.SubscriptionID
}

func NewSubscriptionRequested(subscriptionID, email, repo, confirmToken, unsubToken string) SubscriptionRequested {
	return SubscriptionRequested{
		Metadata: events.Metadata{
			ID:    uuid.NewString(),
			At:    time.Now().UTC(),
			V:     1,
			IdKey: "subscriptions.subscription_requested:" + subscriptionID + ":" + confirmToken,
		},
		SubscriptionID: subscriptionID,
		Email:          email,
		Repo:           repo,
		ConfirmToken:   confirmToken,
		UnsubToken:     unsubToken,
	}
}

func init() {
	events.RegisterType(SubscriptionRequested{})
}
