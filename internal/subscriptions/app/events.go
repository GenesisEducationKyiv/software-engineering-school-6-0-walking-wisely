package subscriptionapp

// SubscriptionRequested is emitted after a watch request is persisted.
type SubscriptionRequested struct {
	SubscriptionID string
	Email          string
	Repo           string
	ConfirmToken   string
	UnsubToken     string
}

func (SubscriptionRequested) EventName() string {
	return "subscriptions.subscription_requested"
}
