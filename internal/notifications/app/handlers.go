package notificationapp

import (
	"context"
	"fmt"
	"strings"

	notificationsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/domain"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	releasemonitoringdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain"
	subscriptionapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app"
)

// Queue accepts email messages for later delivery.
type Queue interface {
	Enqueue(msg mail.Message) bool
}

// EventHandlers react to cross-domain events by enqueueing email work.
type EventHandlers struct {
	queue   Queue
	baseURL string
	log     logger.Logger
}

// NewEventHandlers returns notification handlers backed by the given queue.
func NewEventHandlers(queue Queue, baseURL string, log logger.Logger) *EventHandlers {
	if log == nil {
		log = logger.NoopLogger{}
	}
	return &EventHandlers{queue: queue, baseURL: strings.TrimRight(baseURL, "/"), log: log}
}

// Register attaches all notification handlers to the given bus.
func (h *EventHandlers) Register(bus *events.Bus) {
	bus.Subscribe(subscriptionapp.SubscriptionRequested{}.EventName(), h.OnSubscriptionRequested)
	bus.Subscribe(releasemonitoringdomain.ReleaseDetected{}.EventName(), h.OnReleaseDetected)
}

// OnSubscriptionRequested turns a subscription request into a confirmation email.
func (h *EventHandlers) OnSubscriptionRequested(_ context.Context, event events.Event) error {
	requested, ok := event.(subscriptionapp.SubscriptionRequested)
	if !ok {
		return fmt.Errorf("unexpected event type %T", event)
	}

	notification := notificationsdomain.ConfirmationNotification{
		SubscriptionID: requested.SubscriptionID,
		Email:          requested.Email,
		Repo:           requested.Repo,
		ConfirmToken:   requested.ConfirmToken,
		UnsubToken:     requested.UnsubToken,
	}

	confirmURL := fmt.Sprintf("%s/api/confirm/%s", h.baseURL, notification.ConfirmToken)
	unsubURL := fmt.Sprintf("%s/api/unsubscribe/%s", h.baseURL, notification.UnsubToken)

	if ok := h.queue.Enqueue(mail.Message{
		To:      notification.Email,
		Subject: fmt.Sprintf("Confirm your subscription to %s releases", notification.Repo),
		HTML: fmt.Sprintf(`<p>You requested release notifications for <strong>%s</strong>.</p>
<p><a href="%s">Confirm subscription</a></p>
<p><small>Didn't request this? <a href="%s">Unsubscribe</a></small></p>`,
			notification.Repo, confirmURL, unsubURL),
	}); !ok {
		h.log.Warn("subscribe: email channel full, confirmation email dropped",
			"subscription_id", notification.SubscriptionID,
			"repo", notification.Repo)
		return nil
	}

	h.log.Info("subscribe: confirmation email enqueued",
		"subscription_id", notification.SubscriptionID,
		"repo", notification.Repo)
	return nil
}

// OnReleaseDetected fans a detected release out to all subscribers.
func (h *EventHandlers) OnReleaseDetected(_ context.Context, event events.Event) error {
	detected, ok := event.(releasemonitoringdomain.ReleaseDetected)
	if !ok {
		return fmt.Errorf("unexpected event type %T", event)
	}

	releaseName := detected.Release.TagName
	if detected.Release.Name != "" {
		releaseName = detected.Release.Name
	}

	for _, subscriber := range detected.Subscribers {
		if ok := h.queue.Enqueue(mail.Message{
			To:      subscriber.Email,
			Subject: fmt.Sprintf("[%s] New release: %s", subscriber.Repo, detected.Release.TagName),
			HTML: fmt.Sprintf(`<p>A new release of <strong>%s</strong> is available.</p>
<p><strong>%s</strong></p>
<p><a href="%s">View release on GitHub</a></p>
<hr>
<p><small><a href="%s/api/unsubscribe/%s">Unsubscribe from %s notifications</a></small></p>`,
				subscriber.Repo, releaseName, detected.Release.HTMLURL,
				h.baseURL, subscriber.UnsubscribeToken, subscriber.Repo),
		}); !ok {
			h.log.Warn("scanner: email channel full, dropping notification",
				"subscription_id", subscriber.SubscriptionID,
				"repo", subscriber.Repo)
			continue
		}
	}

	h.log.Info("scanner: release notifications enqueued",
		"repo", detected.Repo,
		"tag", detected.Release.TagName,
		"notified", len(detected.Subscribers))
	return nil
}
