package streams

import (
	"context"
	"encoding/json"
	"fmt"

	goredis "github.com/redis/go-redis/v9"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
)

// Publisher writes domain events to a Redis Stream via XADD.
// It implements events.Publisher and is the outbox dispatcher's delivery target
// in the main API service.
type Publisher struct {
	client    *goredis.Client
	streamKey string
}

func NewPublisher(client *goredis.Client, streamKey string) *Publisher {
	return &Publisher{client: client, streamKey: streamKey}
}

func (p *Publisher) Publish(ctx context.Context, event events.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event %s: %w", event.EventName(), err)
	}

	if err := p.client.XAdd(ctx, &goredis.XAddArgs{
		Stream: p.streamKey,
		MaxLen: 100_000,
		Approx: true,
		ID:     "*",
		Values: map[string]any{
			"event_type": event.EventName(),
			"payload":    string(payload),
		},
	}).Err(); err != nil {
		return fmt.Errorf("xadd to stream %s: %w", p.streamKey, err)
	}

	return nil
}
