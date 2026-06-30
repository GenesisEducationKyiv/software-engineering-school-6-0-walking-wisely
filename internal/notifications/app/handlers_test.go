package notificationapp

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/commands"
	contractevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/events"
	notificationdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/domain"
)

// ── fake JobWriter ─────────────────────────────────────────────────────────────

type fakeJobWriter struct {
	err error

	confirmationCalls        []confirmationArgs
	releaseNotificationCalls []releaseNotificationArgs
}

type confirmationArgs struct {
	handlerName    string
	eventID        string
	subscriptionID string
	sagaID         string
	to             string
	subject        string
	html           string
	confirmToken   string
}

type releaseNotificationArgs struct {
	handlerName string
	eventID     string
	releaseTag  string
	jobs        []notificationdomain.ReleaseNotificationJob
}

func (f *fakeJobWriter) RecordConfirmation(
	_ context.Context,
	handlerName, eventID, subscriptionID, sagaID, to, subject, html, confirmToken string,
) error {
	f.confirmationCalls = append(f.confirmationCalls, confirmationArgs{
		handlerName, eventID, subscriptionID, sagaID, to, subject, html, confirmToken,
	})
	return f.err
}

func (f *fakeJobWriter) RecordReleaseNotifications(
	_ context.Context,
	handlerName, eventID, releaseTag string,
	jobs []notificationdomain.ReleaseNotificationJob,
) error {
	f.releaseNotificationCalls = append(f.releaseNotificationCalls, releaseNotificationArgs{
		handlerName, eventID, releaseTag, jobs,
	})
	return f.err
}

// ── test event factories ───────────────────────────────────────────────────────

func newSendConfirmationEmailCmd() commands.SendConfirmationEmail {
	return commands.NewSendConfirmationEmail(
		uuid.NewString(),
		uuid.NewString(),
		"user@example.com",
		"owner/repo",
		uuid.NewString(),
		uuid.NewString(),
	)
}

func newSubscriber() contractevents.Subscriber {
	return contractevents.Subscriber{
		SubscriptionID:   uuid.NewString(),
		Email:            "a@example.com",
		Repo:             "owner/repo",
		UnsubscribeToken: uuid.NewString(),
	}
}

// ── wrong event type sentinel ──────────────────────────────────────────────────

type wrongEvent struct{}

func (wrongEvent) EventName() string { return "test.wrong" }

// ── OnSendConfirmationEmail ───────────────────────────────────────────────────

func TestOnSendConfirmationEmailHappyPath(t *testing.T) {
	writer := &fakeJobWriter{}
	h := NewEventHandlers(writer, "https://example.com", nil)
	cmd := newSendConfirmationEmailCmd()

	err := h.OnSendConfirmationEmail(context.Background(), cmd)
	if err != nil {
		t.Fatalf("OnSendConfirmationEmail returned error: %v", err)
	}
	if len(writer.confirmationCalls) != 1 {
		t.Fatalf("RecordConfirmation called %d times, want 1", len(writer.confirmationCalls))
	}
	call := writer.confirmationCalls[0]
	if call.handlerName != sendConfirmationEmailHandler {
		t.Errorf("handlerName = %q, want %q", call.handlerName, sendConfirmationEmailHandler)
	}
	if call.eventID != cmd.EventID() {
		t.Errorf("eventID = %q, want %q", call.eventID, cmd.EventID())
	}
	if call.subscriptionID != cmd.SubscriptionID {
		t.Errorf("subscriptionID = %q, want %q", call.subscriptionID, cmd.SubscriptionID)
	}
	if call.sagaID != cmd.SagaID {
		t.Errorf("sagaID = %q, want %q", call.sagaID, cmd.SagaID)
	}
	if call.to != cmd.Email {
		t.Errorf("to = %q, want %q", call.to, cmd.Email)
	}
	if !strings.Contains(call.subject, cmd.Repo) {
		t.Errorf("subject %q does not contain repo %q", call.subject, cmd.Repo)
	}
	wantConfirmURL := "https://example.com/api/confirm/" + cmd.ConfirmToken
	if !strings.Contains(call.html, wantConfirmURL) {
		t.Errorf("html does not contain confirm URL %q", wantConfirmURL)
	}
	wantUnsubURL := "https://example.com/api/unsubscribe/" + cmd.UnsubToken
	if !strings.Contains(call.html, wantUnsubURL) {
		t.Errorf("html does not contain unsub URL %q", wantUnsubURL)
	}
	if call.confirmToken != cmd.ConfirmToken {
		t.Errorf("confirmToken = %q, want %q", call.confirmToken, cmd.ConfirmToken)
	}
}

func TestOnSendConfirmationEmailWrongEventType(t *testing.T) {
	h := NewEventHandlers(&fakeJobWriter{}, "https://example.com", nil)
	err := h.OnSendConfirmationEmail(context.Background(), wrongEvent{})
	if err == nil {
		t.Fatal("expected error for wrong event type, got nil")
	}
}

func TestOnSendConfirmationEmailJobWriterError(t *testing.T) {
	writer := &fakeJobWriter{err: errors.New("storage error")}
	h := NewEventHandlers(writer, "https://example.com", nil)
	cmd := newSendConfirmationEmailCmd()
	err := h.OnSendConfirmationEmail(context.Background(), cmd)
	if err == nil {
		t.Fatal("expected propagated error from JobWriter, got nil")
	}
}

// ── OnReleaseDetected ─────────────────────────────────────────────────────────

func TestOnReleaseDetectedHappyPath(t *testing.T) {
	sub := newSubscriber()
	writer := &fakeJobWriter{}
	h := NewEventHandlers(writer, "https://example.com", nil)
	evt := contractevents.NewReleaseDetected(
		"owner/repo",
		contracts.Release{TagName: "v1.2.3", HTMLURL: "https://github.com/owner/repo/releases/v1.2.3"},
		[]contractevents.Subscriber{sub},
	)

	err := h.OnReleaseDetected(context.Background(), evt)
	if err != nil {
		t.Fatalf("OnReleaseDetected returned error: %v", err)
	}
	if len(writer.releaseNotificationCalls) != 1 {
		t.Fatalf("RecordReleaseNotifications called %d times, want 1", len(writer.releaseNotificationCalls))
	}
	call := writer.releaseNotificationCalls[0]
	if call.handlerName != releaseDetectedHandler {
		t.Errorf("handlerName = %q, want %q", call.handlerName, releaseDetectedHandler)
	}
	if call.eventID != evt.EventID() {
		t.Errorf("eventID = %q, want %q", call.eventID, evt.EventID())
	}
	if call.releaseTag != evt.Release.TagName {
		t.Errorf("releaseTag = %q, want %q", call.releaseTag, evt.Release.TagName)
	}
	if len(call.jobs) != 1 {
		t.Fatalf("jobs count = %d, want 1", len(call.jobs))
	}
	job := call.jobs[0]
	if job.SubscriptionID != sub.SubscriptionID {
		t.Errorf("job.SubscriptionID = %q, want %q", job.SubscriptionID, sub.SubscriptionID)
	}
	if job.To != sub.Email {
		t.Errorf("job.To = %q, want %q", job.To, sub.Email)
	}
	if !strings.Contains(job.Subject, evt.Repo) {
		t.Errorf("subject %q does not contain repo", job.Subject)
	}
	if !strings.Contains(job.Subject, evt.Release.TagName) {
		t.Errorf("subject %q does not contain tag", job.Subject)
	}
	wantUnsubURL := "https://example.com/api/unsubscribe/" + sub.UnsubscribeToken
	if !strings.Contains(job.HTML, wantUnsubURL) {
		t.Errorf("html does not contain unsub URL %q", wantUnsubURL)
	}
}

func TestOnReleaseDetectedUsesReleaseNameWhenNonEmpty(t *testing.T) {
	sub := newSubscriber()
	writer := &fakeJobWriter{}
	h := NewEventHandlers(writer, "https://example.com", nil)
	evt := contractevents.ReleaseDetected{
		Metadata:    contractevents.Metadata{ID: uuid.NewString(), At: time.Now().UTC(), V: 1, IdKey: "key"},
		Repo:        "owner/repo",
		Release:     contracts.Release{TagName: "v1.0.0", Name: "First Release", HTMLURL: "https://github.com"},
		Subscribers: []contractevents.Subscriber{sub},
	}

	err := h.OnReleaseDetected(context.Background(), evt)
	if err != nil {
		t.Fatalf("OnReleaseDetected returned error: %v", err)
	}
	job := writer.releaseNotificationCalls[0].jobs[0]
	if !strings.Contains(job.HTML, "First Release") {
		t.Errorf("html %q should contain release Name when non-empty", job.HTML)
	}
	if strings.Contains(job.HTML, "<strong>v1.0.0</strong>") {
		t.Errorf("html should use Name, not TagName, as the release label when Name is set")
	}
}

func TestOnReleaseDetectedFallsBackToTagNameWhenNameEmpty(t *testing.T) {
	sub := newSubscriber()
	writer := &fakeJobWriter{}
	h := NewEventHandlers(writer, "https://example.com", nil)
	evt := contractevents.ReleaseDetected{
		Metadata:    contractevents.Metadata{ID: uuid.NewString(), At: time.Now().UTC(), V: 1, IdKey: "key"},
		Repo:        "owner/repo",
		Release:     contracts.Release{TagName: "v2.0.0", Name: "", HTMLURL: "https://github.com"},
		Subscribers: []contractevents.Subscriber{sub},
	}

	err := h.OnReleaseDetected(context.Background(), evt)
	if err != nil {
		t.Fatalf("OnReleaseDetected returned error: %v", err)
	}
	job := writer.releaseNotificationCalls[0].jobs[0]
	if !strings.Contains(job.HTML, "v2.0.0") {
		t.Errorf("html %q should contain TagName when Name is empty", job.HTML)
	}
}

func TestOnReleaseDetectedWrongEventType(t *testing.T) {
	h := NewEventHandlers(&fakeJobWriter{}, "https://example.com", nil)
	err := h.OnReleaseDetected(context.Background(), wrongEvent{})
	if err == nil {
		t.Fatal("expected error for wrong event type, got nil")
	}
}

func TestOnReleaseDetectedJobWriterError(t *testing.T) {
	writer := &fakeJobWriter{err: errors.New("storage error")}
	h := NewEventHandlers(writer, "https://example.com", nil)
	evt := contractevents.NewReleaseDetected(
		"owner/repo",
		contracts.Release{TagName: "v1.0.0", HTMLURL: "https://github.com"},
		[]contractevents.Subscriber{newSubscriber()},
	)
	err := h.OnReleaseDetected(context.Background(), evt)
	if err == nil {
		t.Fatal("expected propagated error from JobWriter, got nil")
	}
}

func TestOnReleaseDetectedEmptySubscribers(t *testing.T) {
	writer := &fakeJobWriter{}
	h := NewEventHandlers(writer, "https://example.com", nil)
	evt := contractevents.NewReleaseDetected(
		"owner/repo",
		contracts.Release{TagName: "v1.0.0", HTMLURL: "https://github.com"},
		nil,
	)
	err := h.OnReleaseDetected(context.Background(), evt)
	if err != nil {
		t.Fatalf("OnReleaseDetected returned error: %v", err)
	}
	if len(writer.releaseNotificationCalls) != 1 {
		t.Fatalf("RecordReleaseNotifications called %d times, want 1", len(writer.releaseNotificationCalls))
	}
	if len(writer.releaseNotificationCalls[0].jobs) != 0 {
		t.Fatalf("expected empty jobs slice, got %d", len(writer.releaseNotificationCalls[0].jobs))
	}
}
