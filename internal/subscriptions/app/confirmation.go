package subscriptionapp

import (
	"fmt"
	"log/slog"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
)

// Confirmation is the data needed to notify a user about a pending subscription.
type Confirmation struct {
	Email        string
	Repo         string
	ConfirmToken string
	UnsubToken   string
}

// ConfirmationNotifier turns confirmation requests into queued email messages.
type ConfirmationNotifier struct {
	emailChan chan<- mail.Message
	baseURL   string
}

// NewConfirmationNotifier returns a confirmation email queue adapter.
func NewConfirmationNotifier(emailChan chan<- mail.Message, baseURL string) *ConfirmationNotifier {
	return &ConfirmationNotifier{emailChan: emailChan, baseURL: baseURL}
}

// EnqueueConfirmation queues a confirmation email. A full queue preserves the
// existing best-effort behavior: the subscription succeeds and the drop is logged.
func (n *ConfirmationNotifier) EnqueueConfirmation(c Confirmation) {
	confirmURL := fmt.Sprintf("%s/api/confirm/%s", n.baseURL, c.ConfirmToken)
	unsubURL := fmt.Sprintf("%s/api/unsubscribe/%s", n.baseURL, c.UnsubToken)

	select {
	case n.emailChan <- buildConfirmEmail(c.Email, c.Repo, confirmURL, unsubURL):
	default:
		// Channel full - log with repo only, not email (PII).
		slog.Warn("subscribe: email channel full, confirmation email dropped", "repo", c.Repo)
	}
}

func buildConfirmEmail(email, repo, confirmURL, unsubURL string) mail.Message {
	return mail.Message{
		To:      email,
		Subject: fmt.Sprintf("Confirm your subscription to %s releases", repo),
		HTML: fmt.Sprintf(`<p>You requested release notifications for <strong>%s</strong>.</p>
<p><a href="%s">Confirm subscription</a></p>
<p><small>Didn't request this? <a href="%s">Unsubscribe</a></small></p>`,
			repo, confirmURL, unsubURL),
	}
}
