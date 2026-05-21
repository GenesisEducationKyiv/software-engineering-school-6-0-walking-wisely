package subscriptionapp

import (
	"strings"
	"testing"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
)

type fakeMailQueue struct {
	ok       bool
	messages []mail.Message
}

func (q *fakeMailQueue) Enqueue(msg mail.Message) bool {
	q.messages = append(q.messages, msg)
	return q.ok
}

func TestConfirmationNotifier_NotifyConfirmationBuildsAndQueuesEmail(t *testing.T) {
	queue := &fakeMailQueue{ok: true}
	notifier := NewMailConfirmationNotifier(queue, "http://localhost", nil)

	notifier.NotifyConfirmation(Confirmation{
		Email:        validEmail,
		Repo:         validRepo,
		ConfirmToken: "confirm-token",
		UnsubToken:   "unsub-token",
	})

	if len(queue.messages) != 1 {
		t.Fatalf("queued messages = %d, want 1", len(queue.messages))
	}

	msg := queue.messages[0]
	if msg.To != validEmail {
		t.Errorf("To = %q, want %q", msg.To, validEmail)
	}
	if msg.Subject != "Confirm your subscription to owner/repo releases" {
		t.Errorf("Subject = %q", msg.Subject)
	}
	for _, want := range []string{
		"owner/repo",
		"http://localhost/api/confirm/confirm-token",
		"http://localhost/api/unsubscribe/unsub-token",
	} {
		if !strings.Contains(msg.HTML, want) {
			t.Errorf("HTML does not contain %q: %s", want, msg.HTML)
		}
	}
}

func TestConfirmationNotifier_QueueFullDropsMessage(t *testing.T) {
	queue := &fakeMailQueue{ok: false}
	notifier := NewMailConfirmationNotifier(queue, "http://localhost", nil)

	notifier.NotifyConfirmation(Confirmation{
		Email:        validEmail,
		Repo:         validRepo,
		ConfirmToken: "confirm-token",
		UnsubToken:   "unsub-token",
	})

	if len(queue.messages) != 1 {
		t.Fatalf("enqueue attempts = %d, want 1", len(queue.messages))
	}
}
