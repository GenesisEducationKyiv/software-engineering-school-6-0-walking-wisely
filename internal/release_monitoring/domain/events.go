package domain

// ReleaseDetected is emitted when a repo has a new release for at least one subscriber.
type ReleaseDetected struct {
	Repo        string
	Release     Release
	Subscribers []Subscriber
}

func (ReleaseDetected) EventName() string {
	return "release_monitoring.release_detected"
}
