package domain

// Subscriber is the release-monitoring view of a confirmed watch.
type Subscriber struct {
	SubscriptionID   string
	Email            string
	Repo             string
	UnsubscribeToken string
	LastSeenTag      *string
}
