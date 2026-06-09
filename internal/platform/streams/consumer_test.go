//go:build integration

package streams_test

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"testing"
	"time"

	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/streams"
)

// ── test event ─────────────────────────────────────────────────────────────────

type testEvent struct {
	Value string `json:"value"`
}

func (testEvent) EventName() string { return "test.streams.event" }

func init() {
	events.RegisterType(testEvent{})
}

// ── shared redis setup ─────────────────────────────────────────────────────────

func newRedisClient(t *testing.T) *goredis.Client {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := testcontainers.Run(
		ctx,
		"redis:7-alpine",
		testcontainers.WithExposedPorts("6379/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("6379/tcp")),
	)
	if err != nil {
		t.Fatalf("start redis container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate redis container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("get container host: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("get container port: %v", err)
	}

	client := goredis.NewClient(&goredis.Options{
		Addr: fmt.Sprintf("%s:%s", host, port.Port()),
	})
	t.Cleanup(func() { _ = client.Close() })

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	return client
}

// fakeBus implements events.Publisher by calling a function.
type fakeBus func(context.Context, events.Event) error

func (f fakeBus) Publish(ctx context.Context, ev events.Event) error { return f(ctx, ev) }

// ── Publisher tests ────────────────────────────────────────────────────────────

func TestPublisher_EncodesEventTypeAndPayload(t *testing.T) {
	client := newRedisClient(t)
	pub := streams.NewPublisher(client, "test-stream")

	if err := pub.Publish(context.Background(), testEvent{Value: "hello"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	msgs, err := client.XRange(context.Background(), "test-stream", "-", "+").Result()
	if err != nil {
		t.Fatalf("XRANGE: %v", err)
	}
	if len(msgs) != 1 {
		t.Fatalf("stream length = %d, want 1", len(msgs))
	}

	gotType, _ := msgs[0].Values["event_type"].(string)
	if gotType != "test.streams.event" {
		t.Errorf("event_type = %q, want test.streams.event", gotType)
	}
	payloadStr, _ := msgs[0].Values["payload"].(string)
	var decoded testEvent
	if err := json.Unmarshal([]byte(payloadStr), &decoded); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if decoded.Value != "hello" {
		t.Errorf("decoded.Value = %q, want hello", decoded.Value)
	}
}

func TestPublisher_MultipleEventsAllWritten(t *testing.T) {
	client := newRedisClient(t)
	pub := streams.NewPublisher(client, "test-stream")

	for i := 0; i < 5; i++ {
		if err := pub.Publish(context.Background(), testEvent{Value: fmt.Sprintf("v%d", i)}); err != nil {
			t.Fatalf("Publish %d: %v", i, err)
		}
	}

	n, err := client.XLen(context.Background(), "test-stream").Result()
	if err != nil {
		t.Fatalf("XLEN: %v", err)
	}
	if n != 5 {
		t.Errorf("stream length = %d, want 5", n)
	}
}

// ── Consumer tests ─────────────────────────────────────────────────────────────

func TestConsumer_DeliveredToHandler(t *testing.T) {
	client := newRedisClient(t)
	pub := streams.NewPublisher(client, "test-stream")

	if err := pub.Publish(context.Background(), testEvent{Value: "delivered"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	var received atomic.Int32
	bus := fakeBus(func(_ context.Context, ev events.Event) error {
		if te, ok := ev.(testEvent); ok && te.Value == "delivered" {
			received.Add(1)
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	consumer := streams.NewConsumer(client, "test-stream", "grp", "c1", 32, nil)
	done := make(chan struct{})
	go func() { defer close(done); _ = consumer.Run(ctx, bus) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && received.Load() < 1 {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done

	if received.Load() < 1 {
		t.Error("event was never delivered to handler")
	}
}

func TestConsumer_AcksOnSuccess_NoRedelivery(t *testing.T) {
	// After a successful handler call the message must be ACKed so it is not
	// delivered again to another consumer in the same group.
	client := newRedisClient(t)
	pub := streams.NewPublisher(client, "test-stream")

	if err := pub.Publish(context.Background(), testEvent{Value: "once"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	var count atomic.Int32
	bus := fakeBus(func(_ context.Context, ev events.Event) error {
		if _, ok := ev.(testEvent); ok {
			count.Add(1)
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	consumer := streams.NewConsumer(client, "test-stream", "grp", "c1", 32, nil)
	done := make(chan struct{})
	go func() { defer close(done); _ = consumer.Run(ctx, bus) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && count.Load() < 1 {
		time.Sleep(50 * time.Millisecond)
	}
	// Let it settle to detect any spurious re-delivery.
	time.Sleep(300 * time.Millisecond)
	cancel()
	<-done

	if n := count.Load(); n != 1 {
		t.Errorf("handler called %d times, want exactly 1", n)
	}

	pending, err := client.XPending(context.Background(), "test-stream", "grp").Result()
	if err != nil {
		t.Fatalf("XPENDING: %v", err)
	}
	if pending.Count != 0 {
		t.Errorf("pending count = %d after successful delivery, want 0", pending.Count)
	}
}

func TestConsumer_UnknownEventTypeIsAcked(t *testing.T) {
	// An unknown event type must be ACKed so it doesn't block the PEL.
	client := newRedisClient(t)

	if err := client.XAdd(context.Background(), &goredis.XAddArgs{
		Stream: "test-stream",
		ID:     "*",
		Values: map[string]any{
			"event_type": "no.such.type",
			"payload":    `{}`,
		},
	}).Err(); err != nil {
		t.Fatalf("XADD: %v", err)
	}

	var handlerCalls atomic.Int32
	bus := fakeBus(func(_ context.Context, _ events.Event) error {
		handlerCalls.Add(1)
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	consumer := streams.NewConsumer(client, "test-stream", "grp", "c1", 32, nil)
	done := make(chan struct{})
	go func() { defer close(done); _ = consumer.Run(ctx, bus) }()

	time.Sleep(time.Second)
	cancel()
	<-done

	if handlerCalls.Load() != 0 {
		t.Errorf("handler called %d times for unknown event, want 0", handlerCalls.Load())
	}
	pending, err := client.XPending(context.Background(), "test-stream", "grp").Result()
	if err != nil {
		t.Fatalf("XPENDING: %v", err)
	}
	if pending.Count != 0 {
		t.Errorf("pending count = %d, want 0 (unknown events must be ACKed)", pending.Count)
	}
}

func TestConsumer_HandlerErrorLeavesMessagePending(t *testing.T) {
	// A handler error must NOT ACK the message — it stays in the PEL for redelivery.
	client := newRedisClient(t)
	pub := streams.NewPublisher(client, "test-stream")

	if err := pub.Publish(context.Background(), testEvent{Value: "fail"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	var calls atomic.Int32
	bus := fakeBus(func(_ context.Context, _ events.Event) error {
		calls.Add(1)
		return fmt.Errorf("transient error")
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	consumer := streams.NewConsumer(client, "test-stream", "grp", "c1", 32, nil)
	done := make(chan struct{})
	go func() { defer close(done); _ = consumer.Run(ctx, bus) }()

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && calls.Load() < 1 {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done

	pending, err := client.XPending(context.Background(), "test-stream", "grp").Result()
	if err != nil {
		t.Fatalf("XPENDING: %v", err)
	}
	if pending.Count == 0 {
		t.Error("expected message to remain pending after handler error")
	}
}

func TestConsumer_GroupCreatedIdempotently(t *testing.T) {
	// Running two consumers against the same stream+group must not panic or error.
	client := newRedisClient(t)
	pub := streams.NewPublisher(client, "test-stream")

	if err := pub.Publish(context.Background(), testEvent{Value: "shared"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	var total atomic.Int32
	bus := fakeBus(func(_ context.Context, ev events.Event) error {
		if _, ok := ev.(testEvent); ok {
			total.Add(1)
		}
		return nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c1 := streams.NewConsumer(client, "test-stream", "shared-grp", "c1", 32, nil)
	c2 := streams.NewConsumer(client, "test-stream", "shared-grp", "c2", 32, nil)

	done := make(chan struct{})
	go func() { defer close(done); _ = c1.Run(ctx, bus) }()
	go func() { _ = c2.Run(ctx, bus) }()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && total.Load() < 1 {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	<-done

	// Within a single consumer group exactly one consumer receives each message.
	if n := total.Load(); n != 1 {
		t.Errorf("total deliveries = %d, want exactly 1 (one per group)", n)
	}
}
