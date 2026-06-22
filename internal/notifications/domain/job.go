package domain

type ReleaseNotificationJob struct {
	SubscriptionID string
	To             string
	Subject        string
	HTML           string
}

type Job struct {
	ID           string
	EventID      string
	To           string
	Subject      string
	HTML         string
	AttemptCount int
}
