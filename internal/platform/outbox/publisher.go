package outbox

import (
	"context"
	"fmt"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
)

type publisherRepository interface {
	Append(ctx context.Context, event events.DurableEvent) error
}

type Publisher struct {
	repo publisherRepository
}

func NewPublisher(repo *Repository) *Publisher {
	return &Publisher{repo: repo}
}

func (p *Publisher) Publish(ctx context.Context, event events.Event) error {
	durable, ok := event.(events.DurableEvent)
	if !ok {
		return fmt.Errorf("event %T does not implement DurableEvent", event)
	}
	return p.repo.Append(ctx, durable)
}
