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
	client     *goredis.Client
	streamKey  string
	maxLen     int64
	approxTrim bool
}

func NewPublisher(client *goredis.Client, streamKey string) *Publisher {
	return NewPublisherWithOptions(client, streamKey, PublisherOptions{
		MaxLen:     100_000,
		ApproxTrim: true,
	})
}

type PublisherOptions struct {
	// MaxLen controls Redis Stream trimming. A value <= 0 disables trimming.
	MaxLen int64
	// ApproxTrim uses Redis MAXLEN ~ when MaxLen is enabled.
	ApproxTrim bool
}

func NewPublisherWithOptions(client *goredis.Client, streamKey string, opts PublisherOptions) *Publisher {
	return &Publisher{
		client:     client,
		streamKey:  streamKey,
		maxLen:     opts.MaxLen,
		approxTrim: opts.ApproxTrim,
	}
}

func (p *Publisher) Publish(ctx context.Context, event events.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event %s: %w", event.EventName(), err)
	}

	args := &goredis.XAddArgs{
		Stream: p.streamKey,
		ID:     "*",
		Values: map[string]any{
			"event_type": event.EventName(),
			"payload":    string(payload),
		},
	}
	if p.maxLen > 0 {
		args.MaxLen = p.maxLen
		args.Approx = p.approxTrim
	}

	if err := p.client.XAdd(ctx, args).Err(); err != nil {
		return fmt.Errorf("xadd to stream %s: %w", p.streamKey, err)
	}

	return nil
}
