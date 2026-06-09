package streams

import (
	"context"
	"errors"
	"fmt"
	"strings"
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
	ackTimeout    = 5 * time.Second
)

// Consumer reads domain events from a Redis Stream using a consumer group and
// dispatches them to a local in-process event bus. It provides at-least-once
// delivery: messages are ACKed only after the bus handler returns nil.
// Idle messages (claimed but not ACKed within reclaimAfter) are reclaimed
// periodically to handle crashes without external intervention.
type Consumer struct {
	client        *goredis.Client
	streamKey     string
	group         string
	consumerID    string
	batchSize     int64
	reclaimAfter  time.Duration
	reclaimTick   time.Duration
	ackTimeout    time.Duration
	maxDeliveries int64
	dlqStreamKey  string
	log           logger.Logger
}

type ConsumerOptions struct {
	BatchSize     int64
	ReclaimAfter  time.Duration
	ReclaimTick   time.Duration
	AckTimeout    time.Duration
	MaxDeliveries int64
	DLQStreamKey  string
}

func NewConsumer(
	client *goredis.Client,
	streamKey, group, consumerID string,
	batchSize int64,
	log logger.Logger,
) *Consumer {
	return NewConsumerWithOptions(client, streamKey, group, consumerID, ConsumerOptions{
		BatchSize: batchSize,
	}, log)
}

func NewConsumerWithOptions(
	client *goredis.Client,
	streamKey, group, consumerID string,
	opts ConsumerOptions,
	log logger.Logger,
) *Consumer {
	if log == nil {
		log = logger.NoopLogger{}
	}
	if opts.BatchSize < 1 {
		opts.BatchSize = 32
	}
	if opts.ReclaimAfter <= 0 {
		opts.ReclaimAfter = reclaimAfter
	}
	if opts.ReclaimTick <= 0 {
		opts.ReclaimTick = reclaimTick
	}
	if opts.AckTimeout <= 0 {
		opts.AckTimeout = ackTimeout
	}
	return &Consumer{
		client:        client,
		streamKey:     streamKey,
		group:         group,
		consumerID:    consumerID,
		batchSize:     opts.BatchSize,
		reclaimAfter:  opts.ReclaimAfter,
		reclaimTick:   opts.ReclaimTick,
		ackTimeout:    opts.AckTimeout,
		maxDeliveries: opts.MaxDeliveries,
		dlqStreamKey:  opts.DLQStreamKey,
		log:           log,
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

	reclaimTicker := time.NewTicker(c.reclaimTick)
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
	if err != nil && !strings.Contains(err.Error(), "BUSYGROUP") {
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
		if errors.Is(err, goredis.Nil) {
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
		MinIdle:  c.reclaimAfter,
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
		c.moveToDLQIfExhausted(ctx, msg, eventType, err)
		return
	}

	c.ack(ctx, msg.ID)
}

func (c *Consumer) ack(ctx context.Context, msgID string) {
	baseCtx := ctx
	if ctx.Err() != nil {
		baseCtx = context.WithoutCancel(ctx)
	}
	ackCtx, cancel := context.WithTimeout(baseCtx, c.ackTimeout)
	defer cancel()

	if err := c.client.XAck(ackCtx, c.streamKey, c.group, msgID).Err(); err != nil && ackCtx.Err() == nil {
		c.log.Error("stream consumer ack failed", "msg_id", msgID, "err", err)
	}
}

func (c *Consumer) moveToDLQIfExhausted(ctx context.Context, msg goredis.XMessage, eventType string, cause error) {
	if c.maxDeliveries < 1 || c.dlqStreamKey == "" {
		return
	}

	deliveries, err := c.deliveryCount(ctx, msg.ID)
	if err != nil {
		if ctx.Err() == nil {
			c.log.Error("stream consumer delivery count lookup failed", "msg_id", msg.ID, "err", err)
		}
		return
	}
	if deliveries < c.maxDeliveries {
		return
	}

	dlqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.ackTimeout)
	defer cancel()

	if err := c.client.XAdd(dlqCtx, &goredis.XAddArgs{
		Stream: c.dlqStreamKey,
		ID:     "*",
		Values: map[string]any{
			"original_stream":  c.streamKey,
			"original_group":   c.group,
			"original_id":      msg.ID,
			"event_type":       eventType,
			"payload":          fmt.Sprint(msg.Values["payload"]),
			"deliveries":       deliveries,
			"error":            cause.Error(),
			"dead_lettered_at": time.Now().UTC().Format(time.RFC3339Nano),
		},
	}).Err(); err != nil {
		c.log.Error("stream consumer dlq write failed", "msg_id", msg.ID, "dlq_stream", c.dlqStreamKey, "err", err)
		return
	}

	c.ack(dlqCtx, msg.ID)
	c.log.Error(
		"stream consumer moved message to dlq",
		"event_type", eventType,
		"msg_id", msg.ID,
		"dlq_stream", c.dlqStreamKey,
		"deliveries", deliveries,
		"err", cause,
	)
}

func (c *Consumer) deliveryCount(ctx context.Context, msgID string) (int64, error) {
	pending, err := c.client.XPendingExt(ctx, &goredis.XPendingExtArgs{
		Stream: c.streamKey,
		Group:  c.group,
		Start:  msgID,
		End:    msgID,
		Count:  1,
	}).Result()
	if err != nil {
		return 0, err
	}
	if len(pending) == 0 {
		return 0, nil
	}
	return pending[0].RetryCount, nil
}
