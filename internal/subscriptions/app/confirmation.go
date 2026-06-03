package subscriptionapp

import (
	"fmt"
	"strings"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

// Confirmation is the data needed to notify a user about a pending subscription.
type Confirmation struct {
	SubscriptionID string
	Email          string
	Repo           string
	ConfirmToken   string
	UnsubToken     string
}

// ConfirmationNotifier notifies users about pending subscription confirmations.
type ConfirmationNotifier interface {
	NotifyConfirmation(c Confirmation)
}

// MailConfirmationNotifier turns confirmation requests into email messages.
type MailConfirmationNotifier struct {
	queue   mail.Queue
	baseURL string
	log     logger.Logger
}

// NewMailConfirmationNotifier returns a mail-backed confirmation notifier.
func NewMailConfirmationNotifier(queue mail.Queue, baseURL string, log logger.Logger) *MailConfirmationNotifier {
	if log == nil {
		log = logger.NoopLogger{}
	}
	return &MailConfirmationNotifier{queue: queue, baseURL: baseURL, log: log}
}

// NotifyConfirmation queues a confirmation email. A full queue preserves the
// existing best-effort behavior: the subscription succeeds and the drop is logged.
func (n *MailConfirmationNotifier) NotifyConfirmation(c Confirmation) {
	baseURL := strings.TrimRight(n.baseURL, "/")
	confirmURL := fmt.Sprintf("%s/api/confirm/%s", baseURL, c.ConfirmToken)
	unsubURL := fmt.Sprintf("%s/api/unsubscribe/%s", baseURL, c.UnsubToken)

	if ok := n.queue.Enqueue(buildConfirmEmail(c.Email, c.Repo, confirmURL, unsubURL)); !ok {
		// Channel full - log with repo only, not email (PII).
		n.log.Warn("subscribe: email channel full, confirmation email dropped",
			confirmationLogArgs(c)...)
		return
	}
	n.log.Info("subscribe: confirmation email enqueued", confirmationLogArgs(c)...)
}

func confirmationLogArgs(c Confirmation) []any {
	args := []any{"repo", c.Repo}
	if c.SubscriptionID != "" {
		args = append([]any{"subscription_id", c.SubscriptionID}, args...)
	}
	return args
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
