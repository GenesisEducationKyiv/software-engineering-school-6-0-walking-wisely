package notificationapp

import (
	"context"
	"fmt"
	"strings"

	contractevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/events"
	notificationdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/domain"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

const (
	subscriptionRequestedHandler = "notifications.subscription_requested"
	releaseDetectedHandler       = "notifications.release_detected"
)

// JobWriter persists durable notification jobs.
type JobWriter interface {
	RecordConfirmation(ctx context.Context, handlerName, eventID, subscriptionID, to, subject, html, confirmToken string) error
	RecordReleaseNotifications(ctx context.Context, handlerName, eventID, releaseTag string, jobs []notificationdomain.ReleaseNotificationJob) error
}

// EventHandlers react to cross-domain events by creating durable notification jobs.
type EventHandlers struct {
	jobs    JobWriter
	baseURL string
	log     logger.Logger
}

// NewEventHandlers returns notification handlers backed by durable job storage.
func NewEventHandlers(jobs JobWriter, baseURL string, log logger.Logger) *EventHandlers {
	if log == nil {
		log = logger.NoopLogger{}
	}
	return &EventHandlers{jobs: jobs, baseURL: strings.TrimRight(baseURL, "/"), log: log}
}

// Register attaches all notification handlers to the given bus.
func (h *EventHandlers) Register(bus *events.Bus) {
	bus.Subscribe(contractevents.SubscriptionRequested{}.EventName(), h.OnSubscriptionRequested)
	bus.Subscribe(contractevents.ReleaseDetected{}.EventName(), h.OnReleaseDetected)
}

// OnSubscriptionRequested turns a subscription request into a confirmation email.
func (h *EventHandlers) OnSubscriptionRequested(ctx context.Context, event events.Event) error {
	requested, ok := event.(contractevents.SubscriptionRequested)
	if !ok {
		return fmt.Errorf("unexpected event type %T", event)
	}

	confirmURL := fmt.Sprintf("%s/api/confirm/%s", h.baseURL, requested.ConfirmToken)
	unsubURL := fmt.Sprintf("%s/api/unsubscribe/%s", h.baseURL, requested.UnsubToken)
	subject := fmt.Sprintf("Confirm your subscription to %s releases", requested.Repo)
	html := fmt.Sprintf(`<p>You requested release notifications for <strong>%s</strong>.</p>
<p><a href="%s">Confirm subscription</a></p>
<p><small>Didn't request this? <a href="%s">Unsubscribe</a></small></p>`,
		requested.Repo, confirmURL, unsubURL)

	if err := h.jobs.RecordConfirmation(
		ctx,
		subscriptionRequestedHandler,
		requested.EventID(),
		requested.SubscriptionID,
		requested.Email,
		subject,
		html,
		requested.ConfirmToken,
	); err != nil {
		return fmt.Errorf("record confirmation job: %w", err)
	}

	h.log.Info("subscribe: confirmation job recorded",
		"subscription_id", requested.SubscriptionID,
		"repo", requested.Repo)
	return nil
}

// OnReleaseDetected fans a detected release out to all subscribers.
func (h *EventHandlers) OnReleaseDetected(ctx context.Context, event events.Event) error {
	detected, ok := event.(contractevents.ReleaseDetected)
	if !ok {
		return fmt.Errorf("unexpected event type %T", event)
	}

	releaseName := detected.Release.TagName
	if detected.Release.Name != "" {
		releaseName = detected.Release.Name
	}

	jobs := make([]notificationdomain.ReleaseNotificationJob, 0, len(detected.Subscribers))
	for _, subscriber := range detected.Subscribers {
		jobs = append(jobs, notificationdomain.ReleaseNotificationJob{
			SubscriptionID: subscriber.SubscriptionID,
			To:             subscriber.Email,
			Subject:        fmt.Sprintf("[%s] New release: %s", subscriber.Repo, detected.Release.TagName),
			HTML: fmt.Sprintf(`<p>A new release of <strong>%s</strong> is available.</p>
<p><strong>%s</strong></p>
<p><a href="%s">View release on GitHub</a></p>
<hr>
<p><small><a href="%s/api/unsubscribe/%s">Unsubscribe from %s notifications</a></small></p>`,
				subscriber.Repo, releaseName, detected.Release.HTMLURL,
				h.baseURL, subscriber.UnsubscribeToken, subscriber.Repo),
		})
	}

	if err := h.jobs.RecordReleaseNotifications(
		ctx,
		releaseDetectedHandler,
		detected.EventID(),
		detected.Release.TagName,
		jobs,
	); err != nil {
		return fmt.Errorf("record release jobs: %w", err)
	}

	h.log.Info("scanner: release notification jobs recorded",
		"repo", detected.Repo,
		"tag", detected.Release.TagName,
		"notified", len(detected.Subscribers))
	return nil
}
