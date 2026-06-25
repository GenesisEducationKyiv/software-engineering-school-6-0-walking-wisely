//go:build integration

package nats_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	dockerclient "github.com/moby/moby/client"
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

func startNATSContainer(t *testing.T) (container testcontainers.Container, natsURL string) {
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

	return container, fmt.Sprintf("nats://%s:%s", host, port.Port())
}

func newNATSClient(t *testing.T) *gonats.Conn {
	t.Helper()
	_, natsURL := startNATSContainer(t)

	nc, err := gonats.Connect(natsURL, gonats.NoReconnect())
	if err != nil {
		t.Fatalf("connect nats: %v", err)
	}
	t.Cleanup(nc.Close)
	return nc
}

func TestIntegration_JetStreamPublisherConsumer_DispatchesAndAcksEvent(t *testing.T) {
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

	consumer, err := platformnats.NewConsumer(
		nc, platformlogger.NoopLogger{},
		platformnats.WithStreamName(streamName),
		platformnats.WithSubjectPrefix(subjectPrefix),
		platformnats.WithConsumerName("notifications"),
		platformnats.WithBatchSize(1),
		platformnats.WithAckWait(500*time.Millisecond),
		platformnats.WithMaxDeliveries(3),
		platformnats.WithDLQSubject("events_test_dlq.notifications"),
	)
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

func TestIntegration_JetStreamConsumer_UnknownEventIsAcked(t *testing.T) {
	nc := newNATSClient(t)
	streamName := fmt.Sprintf("TEST_UNKNOWN_%d", time.Now().UnixNano())
	subjectPrefix := "events_unknown"

	consumer, err := platformnats.NewConsumer(
		nc, platformlogger.NoopLogger{},
		platformnats.WithStreamName(streamName),
		platformnats.WithSubjectPrefix(subjectPrefix),
		platformnats.WithConsumerName("notifications"),
		platformnats.WithBatchSize(1),
		platformnats.WithAckWait(500*time.Millisecond),
		platformnats.WithMaxDeliveries(2),
		platformnats.WithDLQSubject("events_unknown_dlq.notifications"),
	)
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

func TestIntegration_JetStreamConsumer_MovesExhaustedMessageToDLQ(t *testing.T) {
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
	consumer, err := platformnats.NewConsumer(
		nc, platformlogger.NoopLogger{},
		platformnats.WithStreamName(streamName),
		platformnats.WithSubjectPrefix(subjectPrefix),
		platformnats.WithConsumerName("notifications"),
		platformnats.WithBatchSize(1),
		platformnats.WithAckWait(200*time.Millisecond),
		platformnats.WithMaxDeliveries(2),
		platformnats.WithDLQSubject(dlqSubject),
	)
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

func TestIntegration_JetStreamConsumer_ReconnectsAfterServerRestart(t *testing.T) {
	container, natsURL := startNATSContainer(t)

	streamName := fmt.Sprintf("TEST_RECONNECT_%d", time.Now().UnixNano())
	subjectPrefix := "events_reconnect"
	log := platformlogger.NoopLogger{}

	// Publisher uses NoReconnect — only needed for initial publish before stop.
	pubNC, err := gonats.Connect(natsURL, gonats.NoReconnect())
	if err != nil {
		t.Fatalf("connect publisher: %v", err)
	}
	defer pubNC.Close()

	pub, err := platformnats.NewPublisher(pubNC, platformnats.PublisherOptions{
		StreamName:    streamName,
		SubjectPrefix: subjectPrefix,
	})
	if err != nil {
		t.Fatalf("NewPublisher: %v", err)
	}

	// Consumer connects via NewClient so it gets reconnect options.
	consNC, err := platformnats.NewClient(natsURL, "test-reconnect-consumer", log)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	defer consNC.Close()

	consumer, err := platformnats.NewConsumer(
		consNC, log,
		platformnats.WithStreamName(streamName),
		platformnats.WithSubjectPrefix(subjectPrefix),
		platformnats.WithConsumerName("notifications"),
		platformnats.WithBatchSize(1),
		platformnats.WithAckWait(2*time.Second),
		platformnats.WithMaxDeliveries(5),
		platformnats.WithDLQSubject(subjectPrefix+"_dlq.notifications"),
	)
	if err != nil {
		t.Fatalf("NewConsumer: %v", err)
	}

	if err := pub.Publish(context.Background(), testEvent{Value: "reconnect"}); err != nil {
		t.Fatalf("Publish: %v", err)
	}

	// Pause container to simulate network loss without changing the mapped port.
	dockerCli, err := dockerclient.NewClientWithOpts(dockerclient.FromEnv, dockerclient.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("docker client: %v", err)
	}
	defer dockerCli.Close()

	containerID := container.GetContainerID()

	pauseCtx, pauseCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer pauseCancel()
	if _, err := dockerCli.ContainerPause(pauseCtx, containerID, dockerclient.ContainerPauseOptions{}); err != nil {
		t.Fatalf("pause container: %v", err)
	}
	time.Sleep(500 * time.Millisecond)

	unpauseCtx, unpauseCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer unpauseCancel()
	if _, err := dockerCli.ContainerUnpause(unpauseCtx, containerID, dockerclient.ContainerUnpauseOptions{}); err != nil {
		t.Fatalf("unpause container: %v", err)
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

	select {
	case ev := <-got:
		if ev.Value != "reconnect" {
			t.Fatalf("event value = %q, want reconnect", ev.Value)
		}
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for event after server restart")
	}
}
