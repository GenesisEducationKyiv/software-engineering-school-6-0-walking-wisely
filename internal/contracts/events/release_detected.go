package events

import (
	"time"

	"github.com/google/uuid"

	platformevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
)

// Release is the release payload shared with event consumers.
type Release struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
	Name    string `json:"name"`
}

// Subscriber is the notification recipient payload shared with event consumers.
type Subscriber struct {
	SubscriptionID   string
	Email            string
	Repo             string
	UnsubscribeToken string
	LastSeenTag      *string
}

// ReleaseDetected is emitted when a repo has a new release for at least one subscriber.
type ReleaseDetected struct {
	platformevents.Metadata
	Repo        string
	Release     Release
	Subscribers []Subscriber
}

func (ReleaseDetected) EventName() string {
	return "release_monitoring.release_detected"
}

func (ReleaseDetected) AggregateType() string {
	return "repository_release"
}

// Keep a value receiver so decoded events can still be asserted as ReleaseDetected.
//
//nolint:gocritic // Durable event decoding intentionally preserves value event types.
func (e ReleaseDetected) AggregateID() string {
	return e.Repo
}

func NewReleaseDetected(repo string, release Release, subscribers []Subscriber) ReleaseDetected {
	return ReleaseDetected{
		Metadata: platformevents.Metadata{
			ID:    uuid.NewString(),
			At:    time.Now().UTC(),
			V:     1,
			IdKey: "release_monitoring.release_detected:" + repo + ":" + release.TagName,
		},
		Repo:        repo,
		Release:     release,
		Subscribers: subscribers,
	}
}

func init() {
	platformevents.RegisterType(ReleaseDetected{})
}
