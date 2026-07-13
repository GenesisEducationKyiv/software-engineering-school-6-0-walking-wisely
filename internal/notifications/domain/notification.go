package domain

// ConfirmationNotification requests a subscription confirmation email.
type ConfirmationNotification struct {
	SubscriptionID string
	Email          string
	Repo           string
	ConfirmToken   string
	UnsubToken     string
}
