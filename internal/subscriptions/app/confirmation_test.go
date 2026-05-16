package subscriptionapp

import (
	"strings"
	"testing"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
)

func TestConfirmationNotifier_EnqueueConfirmation(t *testing.T) {
	ch := make(chan mail.Message, 1)
	notifier := NewConfirmationNotifier(ch, "http://localhost")

	notifier.EnqueueConfirmation(Confirmation{
		Email:        validEmail,
		Repo:         validRepo,
		ConfirmToken: "confirm-token",
		UnsubToken:   "unsub-token",
	})

	if len(ch) != 1 {
		t.Fatalf("queued messages = %d, want 1", len(ch))
	}

	msg := <-ch
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
	ch := make(chan mail.Message)
	notifier := NewConfirmationNotifier(ch, "http://localhost")

	notifier.EnqueueConfirmation(Confirmation{
		Email:        validEmail,
		Repo:         validRepo,
		ConfirmToken: "confirm-token",
		UnsubToken:   "unsub-token",
	})

	if len(ch) != 0 {
		t.Errorf("queued messages = %d, want 0", len(ch))
	}
}
