package subscriptionapp

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
)

type recordingConfirmationLogger struct {
	warnings []recordedWarning
}

type recordedWarning struct {
	msg  string
	args []any
}

func (l *recordingConfirmationLogger) Debug(string, ...any) {}

func (l *recordingConfirmationLogger) Info(string, ...any) {}

func (l *recordingConfirmationLogger) Warn(msg string, args ...any) {
	l.warnings = append(l.warnings, recordedWarning{msg: msg, args: append([]any(nil), args...)})
}

func (l *recordingConfirmationLogger) Error(string, ...any) {}

func (l *recordingConfirmationLogger) ErrorContext(context.Context, string, ...any) {}

type fakeMailQueue struct {
	ok       bool
	messages []mail.Message
}

func (q *fakeMailQueue) Enqueue(msg mail.Message) bool {
	q.messages = append(q.messages, msg)
	return q.ok
}

func TestConfirmationNotifier_EnqueueConfirmation(t *testing.T) {
	queue := &fakeMailQueue{ok: true}
	log := &recordingConfirmationLogger{}
	notifier := NewMailConfirmationNotifier(queue, "http://localhost", log)

	notifier.NotifyConfirmation(&Confirmation{
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
	if len(log.warnings) != 0 {
		t.Errorf("warnings = %d, want 0", len(log.warnings))
	}
}

func TestConfirmationNotifier_QueueFullDropsMessage(t *testing.T) {
	queue := &fakeMailQueue{ok: false}
	log := &recordingConfirmationLogger{}
	notifier := NewMailConfirmationNotifier(queue, "http://localhost", log)

	notifier.NotifyConfirmation(&Confirmation{
		Email:        validEmail,
		Repo:         validRepo,
		ConfirmToken: "confirm-token",
		UnsubToken:   "unsub-token",
	})

	if len(queue.messages) != 1 {
		t.Fatalf("enqueue attempts = %d, want 1", len(queue.messages))
	}
	if len(log.warnings) != 1 {
		t.Fatalf("warnings = %d, want 1", len(log.warnings))
	}

	warning := log.warnings[0]
	if warning.msg != "subscribe: email channel full, confirmation email dropped" {
		t.Errorf("warning message = %q", warning.msg)
	}
	if len(warning.args) != 2 || warning.args[0] != "repo" || warning.args[1] != validRepo {
		t.Errorf("warning args = %#v, want repo only", warning.args)
	}

	warningText := fmt.Sprint(warning.msg, warning.args)
	if strings.Contains(warningText, validEmail) {
		t.Errorf("warning contains email PII: %q", warningText)
	}
}

func TestConfirmationNotifier_EnqueueConfirmation_TrimsTrailingBaseURLSlash(t *testing.T) {
	queue := &fakeMailQueue{ok: true}
	log := &recordingConfirmationLogger{}
	notifier := NewMailConfirmationNotifier(queue, "http://localhost/", log)

	notifier.NotifyConfirmation(&Confirmation{
		Email:        validEmail,
		Repo:         validRepo,
		ConfirmToken: "confirm-token",
		UnsubToken:   "unsub-token",
	})

	msg := queue.messages[0]
	for _, want := range []string{
		"http://localhost/api/confirm/confirm-token",
		"http://localhost/api/unsubscribe/unsub-token",
	} {
		if !strings.Contains(msg.HTML, want) {
			t.Errorf("HTML does not contain %q: %s", want, msg.HTML)
		}
	}
	for _, notWant := range []string{
		"http://localhost//api/confirm/confirm-token",
		"http://localhost//api/unsubscribe/unsub-token",
	} {
		if strings.Contains(msg.HTML, notWant) {
			t.Errorf("HTML contains double-slash URL %q: %s", notWant, msg.HTML)
		}
	}
	if len(log.warnings) != 0 {
		t.Errorf("warnings = %d, want 0", len(log.warnings))
	}
}

func TestConfirmationNotifier_QueueFullWithNilLoggerDoesNotPanic(t *testing.T) {
	queue := &fakeMailQueue{ok: false}
	notifier := NewMailConfirmationNotifier(queue, "http://localhost", nil)

	notifier.NotifyConfirmation(&Confirmation{
		Email:        validEmail,
		Repo:         validRepo,
		ConfirmToken: "confirm-token",
		UnsubToken:   "unsub-token",
	})

	if len(queue.messages) != 1 {
		t.Fatalf("enqueue attempts = %d, want 1", len(queue.messages))
	}
}
