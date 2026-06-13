//go:build integration

package nats_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	gonats "github.com/nats-io/nats.go"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	platformlogger "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	platformnats "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/nats"
)

type testEvent struct {
	Value string `json:"value"`
}

func (testEvent) EventName() string { return "test.nats.event" }

func init() {
	events.RegisterType(testEvent{})
}

type fakeBus func(context.Context, events.Event) error

func (f fakeBus) Publish(ctx context.Context, ev events.Event) error { return f(ctx, ev) }

func newNATSClient(t *testing.T) *gonats.Conn {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	container, err := testcontainers.Run(
		ctx,
		"nats:2.11-alpine",
		testcontainers.WithExposedPorts("4222/tcp"),
		testcontainers.WithCmd("-js", "-sd", "/data"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("4222/tcp")),
	)
	if err != nil {
		t.Fatalf("start nats container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate nats container: %v", err)
		}
	})

	host, err := container.Host(ctx)
	if err != nil {
		t.Fatalf("get nats host: %v", err)
	}
	port, err := container.MappedPort(ctx, "4222/tcp")
	if err != nil {
		t.Fatalf("get nats port: %v", err)
	}

	nc, err := gonats.Connect(fmt.Sprintf("nats://%s:%s", host, port.Port()), gonats.NoReconnect())
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

func TestJetStreamPublisherConsumer_DispatchesAndAcksEvent(t *testing.T) {
	nc := newNATSClient(t)
	streamName := fmt.Sprintf("TEST_EVENTS_%d", time.Now().UnixNano())
	subjectPrefix := "events_test"

	pub, err := platformnats.NewPublisher(nc, platformnats.PublisherOptions{
		StreamName:    streamName,
		SubjectPrefix: subjectPrefix,
	})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}

	consumer, err := platformnats.NewConsumer(nc, &platformnats.ConsumerOptions{
		StreamName:    streamName,
		SubjectPrefix: subjectPrefix,
		ConsumerName:  "notifications",
		BatchSize:     1,
		AckWait:       500 * time.Millisecond,
		MaxDeliveries: 3,
		DLQSubject:    "events_test_dlq.notifications",
	}, platformlogger.NoopLogger{})
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}

	got := make(chan testEvent, 1)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = consumer.Run(ctx, fakeBus(func(_ context.Context, ev events.Event) error {
			got <- ev.(testEvent)
			cancel()
			return nil
		}))
	}()

	if err := pub.Publish(context.Background(), testEvent{Value: "hello"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	select {
	case ev := <-got:
		if ev.Value != "hello" {
			t.Fatalf("event value = %q, want hello", ev.Value)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for event")
	}
}

func TestJetStreamConsumer_UnknownEventIsAcked(t *testing.T) {
	nc := newNATSClient(t)
	streamName := fmt.Sprintf("TEST_UNKNOWN_%d", time.Now().UnixNano())
	subjectPrefix := "events_unknown"

	consumer, err := platformnats.NewConsumer(nc, &platformnats.ConsumerOptions{
		StreamName:    streamName,
		SubjectPrefix: subjectPrefix,
		ConsumerName:  "notifications",
		BatchSize:     1,
		AckWait:       500 * time.Millisecond,
		MaxDeliveries: 2,
		DLQSubject:    "events_unknown_dlq.notifications",
	}, platformlogger.NoopLogger{})
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}
	envelope, err := json.Marshal(map[string]any{
		"event_type": "unknown.event",
		"payload":    map[string]string{"value": "ignored"},
	})
	if err != nil {
		t.Fatalf("marshal envelope: %v", err)
	}
	if _, err := js.Publish(subjectPrefix+".unknown", envelope); err != nil {
		t.Fatalf("publish unknown event: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = consumer.Run(ctx, fakeBus(func(context.Context, events.Event) error {
			t.Fatal("unknown event should not reach bus")
			return nil
		}))
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, err := js.ConsumerInfo(streamName, "notifications")
		if err == nil && info.NumAckPending == 0 && info.NumPending == 0 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("unknown event was not acked")
}

func TestJetStreamConsumer_MovesExhaustedMessageToDLQ(t *testing.T) {
	nc := newNATSClient(t)
	streamName := fmt.Sprintf("TEST_DLQ_%d", time.Now().UnixNano())
	subjectPrefix := "events_dlq_test"
	dlqSubject := "dead_events_dlq_test.notifications"

	pub, err := platformnats.NewPublisher(nc, platformnats.PublisherOptions{
		StreamName:    streamName,
		SubjectPrefix: subjectPrefix,
	})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}
	consumer, err := platformnats.NewConsumer(nc, &platformnats.ConsumerOptions{
		StreamName:    streamName,
		SubjectPrefix: subjectPrefix,
		ConsumerName:  "notifications",
		BatchSize:     1,
		AckWait:       200 * time.Millisecond,
		MaxDeliveries: 2,
		DLQSubject:    dlqSubject,
	}, platformlogger.NoopLogger{})
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		_ = consumer.Run(ctx, fakeBus(func(context.Context, events.Event) error {
			return errors.New("handler failed")
		}))
	}()

	if err := pub.Publish(context.Background(), testEvent{Value: "bad"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	js, err := nc.JetStream()
	if err != nil {
		t.Fatalf("JetStream: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		info, err := js.StreamInfo(streamName + "_DLQ")
		if err == nil && info.State.Msgs == 1 {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("message was not moved to DLQ")
}
