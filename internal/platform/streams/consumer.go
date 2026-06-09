package streams

import (
	"context"
	"time"

	goredis "github.com/redis/go-redis/v9"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

const (
	blockDuration = 500 * time.Millisecond
	reclaimAfter  = 5 * time.Minute
	reclaimTick   = 5 * time.Minute
	errorBackoff  = 500 * time.Millisecond
)

// Consumer reads domain events from a Redis Stream using a consumer group and
// dispatches them to a local in-process event bus. It provides at-least-once
// delivery: messages are ACKed only after the bus handler returns nil.
// Idle messages (claimed but not ACKed within reclaimAfter) are reclaimed
// periodically to handle crashes without external intervention.
type Consumer struct {
	client     *goredis.Client
	streamKey  string
	group      string
	consumerID string
	batchSize  int64
	log        logger.Logger
}

func NewConsumer(
	client *goredis.Client,
	streamKey, group, consumerID string,
	batchSize int64,
	log logger.Logger,
) *Consumer {
	if log == nil {
		log = logger.NoopLogger{}
	}
	if batchSize < 1 {
		batchSize = 32
	}
	return &Consumer{
		client:     client,
		streamKey:  streamKey,
		group:      group,
		consumerID: consumerID,
		batchSize:  batchSize,
		log:        log,
	}
}

// Run starts the consume loop. It creates the consumer group if needed, then
// reads and dispatches batches until ctx is cancelled.
func (c *Consumer) Run(ctx context.Context, bus events.Publisher) error {
	if err := c.ensureGroup(ctx); err != nil {
		return err
	}

	c.log.Info(
		"stream consumer started",
		"stream", c.streamKey,
		"group", c.group,
		"consumer", c.consumerID,
	)

	reclaimTicker := time.NewTicker(reclaimTick)
	defer reclaimTicker.Stop()

	for {
		// Non-blocking check for reclaim tick or cancellation before each read.
		select {
		case <-ctx.Done():
			c.log.Info("stream consumer stopped")
			return nil
		case <-reclaimTicker.C:
			c.reclaimIdle(ctx, bus)
		default:
		}

		if err := c.readBatch(ctx, bus); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			c.log.Error("stream consumer batch failed", "err", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(errorBackoff):
			}
		}
	}
}

func (c *Consumer) ensureGroup(ctx context.Context) error {
	err := c.client.XGroupCreateMkStream(ctx, c.streamKey, c.group, "0").Err()
	if err != nil && err.Error() != "BUSYGROUP Consumer Group name already exists" {
		return err
	}
	return nil
}

func (c *Consumer) readBatch(ctx context.Context, bus events.Publisher) error {
	result, err := c.client.XReadGroup(ctx, &goredis.XReadGroupArgs{
		Group:    c.group,
		Consumer: c.consumerID,
		Streams:  []string{c.streamKey, ">"},
		Count:    c.batchSize,
		Block:    blockDuration,
	}).Result()
	if err != nil {
		if err == goredis.Nil {
			return nil // block timeout, no new messages
		}
		return err
	}

	for _, s := range result {
		for _, msg := range s.Messages {
			c.dispatch(ctx, bus, msg)
		}
	}
	return nil
}

func (c *Consumer) reclaimIdle(ctx context.Context, bus events.Publisher) {
	msgs, _, err := c.client.XAutoClaim(ctx, &goredis.XAutoClaimArgs{
		Stream:   c.streamKey,
		Group:    c.group,
		Consumer: c.consumerID,
		MinIdle:  reclaimAfter,
		Start:    "0-0",
		Count:    c.batchSize,
	}).Result()
	if err != nil {
		if ctx.Err() == nil {
			c.log.Error("stream consumer reclaim failed", "err", err)
		}
		return
	}
	for _, msg := range msgs {
		c.dispatch(ctx, bus, msg)
	}
}

// dispatch decodes a stream message and publishes it to the bus.
// On success it ACKs the message. On handler failure it leaves the message
// unACKed so it will be redelivered (via reclaimIdle).
// Unknown event types are ACKed immediately to prevent blocking the stream.
func (c *Consumer) dispatch(ctx context.Context, bus events.Publisher, msg goredis.XMessage) {
	eventType, _ := msg.Values["event_type"].(string)
	payloadStr, _ := msg.Values["payload"].(string)

	event, err := events.Decode(eventType, []byte(payloadStr))
	if err != nil {
		c.log.Warn("stream consumer unknown event type, skipping",
			"event_type", eventType, "msg_id", msg.ID)
		c.ack(ctx, msg.ID)
		return
	}

	if err := bus.Publish(ctx, event); err != nil {
		c.log.Error("stream consumer handler failed, message will be redelivered",
			"event_type", eventType, "msg_id", msg.ID, "err", err)
		return
	}

	c.ack(ctx, msg.ID)
}

func (c *Consumer) ack(ctx context.Context, msgID string) {
	if err := c.client.XAck(ctx, c.streamKey, c.group, msgID).Err(); err != nil && ctx.Err() == nil {
		c.log.Error("stream consumer ack failed", "msg_id", msgID, "err", err)
	}
}
