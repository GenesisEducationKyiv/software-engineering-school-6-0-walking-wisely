package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	gonats "github.com/nats-io/nats.go"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

const (
	defaultFetchWait = 500 * time.Millisecond
	defaultAckWait   = 5 * time.Second
	defaultBatchSize = 32
	errorBackoff     = 500 * time.Millisecond
)

type eventEnvelope struct {
	EventType string          `json:"event_type"`
	Payload   json.RawMessage `json:"payload"`
}

// Publisher writes domain events to a NATS JetStream stream.
type Publisher struct {
	js            gonats.JetStreamContext
	streamName    string
	subjectPrefix string
}

type PublisherOptions struct {
	StreamName    string
	SubjectPrefix string
}

func NewPublisher(nc *gonats.Conn, opts PublisherOptions) (*Publisher, error) {
	if opts.StreamName == "" {
		opts.StreamName = "EVENTS"
	}
	if opts.SubjectPrefix == "" {
		opts.SubjectPrefix = "events"
	}
	opts.SubjectPrefix = strings.Trim(opts.SubjectPrefix, ".")

	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("init jetstream: %w", err)
	}

	if err := ensureStream(js, opts.StreamName, opts.SubjectPrefix+".>"); err != nil {
		return nil, err
	}

	return &Publisher{
		js:            js,
		streamName:    opts.StreamName,
		subjectPrefix: opts.SubjectPrefix,
	}, nil
}

func (p *Publisher) Publish(ctx context.Context, event events.Event) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event %s: %w", event.EventName(), err)
	}

	envelope, err := json.Marshal(eventEnvelope{
		EventType: event.EventName(),
		Payload:   payload,
	})
	if err != nil {
		return fmt.Errorf("marshal envelope %s: %w", event.EventName(), err)
	}

	subject := p.subjectPrefix + "." + event.EventName()
	if _, err := p.js.PublishMsg(&gonats.Msg{
		Subject: subject,
		Data:    envelope,
		Header: gonats.Header{
			"event-type": []string{event.EventName()},
		},
	}, gonats.Context(ctx)); err != nil {
		return fmt.Errorf("publish %s to stream %s: %w", subject, p.streamName, err)
	}

	return nil
}

// Consumer reads events from a JetStream durable pull consumer and dispatches
// them to the provided in-process event bus.
type Consumer struct {
	js            gonats.JetStreamContext
	streamName    string
	subjectPrefix string
	consumerName  string
	batchSize     int
	ackWait       time.Duration
	maxDeliveries int
	dlqSubject    string
	fetchWait     time.Duration
	subscription  *gonats.Subscription
	log           logger.Logger
}

type ConsumerOptions struct {
	StreamName    string
	SubjectPrefix string
	ConsumerName  string
	BatchSize     int
	AckWait       time.Duration
	MaxDeliveries int
	DLQSubject    string
	FetchWait     time.Duration
}

func NewConsumer(nc *gonats.Conn, opts *ConsumerOptions, log logger.Logger) (*Consumer, error) {
	if log == nil {
		log = logger.NoopLogger{}
	}
	if opts == nil {
		opts = &ConsumerOptions{}
	}
	if opts.StreamName == "" {
		opts.StreamName = "EVENTS"
	}
	if opts.SubjectPrefix == "" {
		opts.SubjectPrefix = "events"
	}
	if opts.ConsumerName == "" {
		opts.ConsumerName = "notifications"
	}
	if opts.BatchSize < 1 {
		opts.BatchSize = defaultBatchSize
	}
	if opts.AckWait <= 0 {
		opts.AckWait = defaultAckWait
	}
	if opts.FetchWait <= 0 {
		opts.FetchWait = defaultFetchWait
	}
	if opts.DLQSubject == "" {
		opts.DLQSubject = "events_dlq.notifications"
	}
	opts.SubjectPrefix = strings.Trim(opts.SubjectPrefix, ".")

	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("init jetstream: %w", err)
	}
	if err := ensureStream(js, opts.StreamName, opts.SubjectPrefix+".>"); err != nil {
		return nil, err
	}
	if opts.DLQSubject != "" {
		if err := ensureStream(js, opts.StreamName+"_DLQ", opts.DLQSubject); err != nil {
			return nil, err
		}
	}

	return &Consumer{
		js:            js,
		streamName:    opts.StreamName,
		subjectPrefix: opts.SubjectPrefix,
		consumerName:  opts.ConsumerName,
		batchSize:     opts.BatchSize,
		ackWait:       opts.AckWait,
		maxDeliveries: opts.MaxDeliveries,
		dlqSubject:    opts.DLQSubject,
		fetchWait:     opts.FetchWait,
		log:           log,
	}, nil
}

func (c *Consumer) Run(ctx context.Context, bus events.Publisher) error {
	sub, err := c.ensureSubscription()
	if err != nil {
		return err
	}
	c.subscription = sub
	defer func() {
		if err := sub.Drain(); err != nil {
			c.log.Error("jetstream consumer drain failed", "err", err)
		}
	}()

	c.log.Info(
		"jetstream consumer started",
		"stream", c.streamName,
		"consumer", c.consumerName,
		"subject", c.subjectPrefix+".>",
	)

	for {
		select {
		case <-ctx.Done():
			c.log.Info("jetstream consumer stopped")
			return nil
		default:
		}

		msgs, err := sub.Fetch(c.batchSize, gonats.MaxWait(c.fetchWait))
		if err != nil {
			if errors.Is(err, gonats.ErrTimeout) {
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			c.log.Error("jetstream consumer fetch failed", "err", err)
			select {
			case <-ctx.Done():
				return nil
			case <-time.After(errorBackoff):
			}
			continue
		}

		for _, msg := range msgs {
			c.dispatch(ctx, bus, msg)
		}
	}
}

func (c *Consumer) ensureSubscription() (*gonats.Subscription, error) {
	subject := c.subjectPrefix + ".>"
	opts := []gonats.SubOpt{
		gonats.BindStream(c.streamName),
		gonats.ManualAck(),
		gonats.AckWait(c.ackWait),
	}
	if c.maxDeliveries > 0 {
		opts = append(opts, gonats.MaxDeliver(c.maxDeliveries))
	}

	sub, err := c.js.PullSubscribe(subject, c.consumerName, opts...)
	if err != nil {
		return nil, fmt.Errorf("create jetstream pull consumer %s: %w", c.consumerName, err)
	}
	return sub, nil
}

func (c *Consumer) dispatch(ctx context.Context, bus events.Publisher, msg *gonats.Msg) {
	var envelope eventEnvelope
	if err := json.Unmarshal(msg.Data, &envelope); err != nil {
		c.log.Warn("jetstream consumer invalid envelope, skipping", "subject", msg.Subject, "err", err)
		c.ack(ctx, msg)
		return
	}

	event, err := events.Decode(envelope.EventType, envelope.Payload)
	if err != nil {
		c.log.Warn(
			"jetstream consumer unknown event type, skipping",
			"event_type", envelope.EventType,
			"subject", msg.Subject,
		)
		c.ack(ctx, msg)
		return
	}

	if err := bus.Publish(ctx, event); err != nil {
		c.log.Error(
			"jetstream consumer handler failed, message will be redelivered",
			"event_type", envelope.EventType,
			"subject", msg.Subject,
			"err", err,
		)
		c.moveToDLQIfExhausted(ctx, msg, envelope, err)
		return
	}

	c.ack(ctx, msg)
}

func (c *Consumer) ack(ctx context.Context, msg *gonats.Msg) {
	if ctx.Err() != nil {
		ctx = context.WithoutCancel(ctx)
	}
	ackCtx, cancel := context.WithTimeout(ctx, c.ackWait)
	defer cancel()

	if err := msg.Ack(gonats.Context(ackCtx)); err != nil && ackCtx.Err() == nil {
		c.log.Error("jetstream consumer ack failed", "subject", msg.Subject, "err", err)
	}
}

func (c *Consumer) moveToDLQIfExhausted(ctx context.Context, msg *gonats.Msg, envelope eventEnvelope, cause error) {
	if c.maxDeliveries < 1 || c.dlqSubject == "" {
		return
	}

	metadata, err := msg.Metadata()
	if err != nil {
		c.log.Error("jetstream consumer metadata lookup failed", "subject", msg.Subject, "err", err)
		return
	}
	if metadata.NumDelivered < uint64(c.maxDeliveries) {
		return
	}

	dlqPayload, err := json.Marshal(struct {
		OriginalSubject string          `json:"original_subject"`
		EventType       string          `json:"event_type"`
		Payload         json.RawMessage `json:"payload"`
		Deliveries      uint64          `json:"deliveries"`
		Error           string          `json:"error"`
		DeadLetteredAt  time.Time       `json:"dead_lettered_at"`
	}{
		OriginalSubject: msg.Subject,
		EventType:       envelope.EventType,
		Payload:         envelope.Payload,
		Deliveries:      metadata.NumDelivered,
		Error:           cause.Error(),
		DeadLetteredAt:  time.Now().UTC(),
	})
	if err != nil {
		c.log.Error("jetstream consumer dlq marshal failed", "subject", msg.Subject, "err", err)
		return
	}

	dlqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.ackWait)
	defer cancel()
	if _, err := c.js.Publish(c.dlqSubject, dlqPayload, gonats.Context(dlqCtx)); err != nil {
		c.log.Error("jetstream consumer dlq publish failed", "subject", msg.Subject, "dlq_subject", c.dlqSubject, "err", err)
		return
	}

	c.ack(dlqCtx, msg)
	c.log.Error(
		"jetstream consumer moved message to dlq",
		"event_type", envelope.EventType,
		"subject", msg.Subject,
		"dlq_subject", c.dlqSubject,
		"deliveries", metadata.NumDelivered,
		"err", cause,
	)
}

func ensureStream(js gonats.JetStreamContext, name, subject string) error {
	cfg := &gonats.StreamConfig{
		Name:     name,
		Subjects: []string{subject},
		Storage:  gonats.FileStorage,
	}

	if _, err := js.StreamInfo(name); err != nil {
		if _, addErr := js.AddStream(cfg); addErr != nil {
			return fmt.Errorf("create jetstream stream %s: %w", name, addErr)
		}
		return nil
	}

	if _, err := js.UpdateStream(cfg); err != nil {
		return fmt.Errorf("update jetstream stream %s: %w", name, err)
	}
	return nil
}
