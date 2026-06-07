package domain

// Release is the release information needed to notify subscribers.
type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Name    string `json:"name"`
}

// Subscriber is the release-monitoring view of a confirmed watch.
type Subscriber struct {
	SubscriptionID   string
	Email            string
	Repo             string
	UnsubscribeToken string
	LastSeenTag      *string
}
