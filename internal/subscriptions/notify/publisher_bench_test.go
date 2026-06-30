package notify_test

import (
	"context"
	"net"
	"os"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/google/uuid"

	notificationv1 "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/notification/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/commands"
	contractevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/events"
	platformevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	platformnats "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/nats"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/notify"
)

// noopNotificationServer satisfies NotificationServiceServer with a trivial
// handler (no insert) for a clean transport micro-bench (Layer A headline). The
// slow/recovering-consumer collapse scenario is driven over the wire instead —
// see scripts/bench-grpc-slow against cmd/notifications-bench.
type noopNotificationServer struct {
	notificationv1.UnimplementedNotificationServiceServer
}

func (noopNotificationServer) SendConfirmation(_ context.Context, _ *notificationv1.SendConfirmationRequest) (*notificationv1.Ack, error) {
	return &notificationv1.Ack{}, nil
}

// newGRPCPublisher starts an in-process gRPC server on a loopback TCP port and
// returns a GRPCPublisher wired to it. Real loopback network hop through the gRPC
// stack — realistic transport overhead, minus the handler (stub server, see below).
//
//nolint:gocritic // unnamedResult: helper returns two values, naming adds no clarity
func newGRPCPublisher(b *testing.B) (*notify.GRPCPublisher, func()) {
	b.Helper()

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		b.Fatalf("listen: %v", err)
	}

	srv := grpc.NewServer()
	notificationv1.RegisterNotificationServiceServer(srv, noopNotificationServer{})
	go srv.Serve(lis) //nolint:errcheck // benchmark server; error on stop is expected and irrelevant

	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		srv.Stop()
		b.Fatalf("dial: %v", err)
	}

	client := notificationv1.NewNotificationServiceClient(conn)
	pub := notify.NewGRPCPublisher(client, 64)
	cleanup := func() {
		_ = conn.Close()
		srv.Stop()
	}
	return pub, cleanup
}

// newNATSPublisher connects to NATS at BENCHMARK_NATS_URL (or NATS_URL) and
// returns a Publisher. Skips the benchmark if no URL is set.
//
//nolint:gocritic // unnamedResult: helper returns two values, naming adds no clarity
func newNATSPublisher(b *testing.B) (platformevents.Publisher, func()) {
	b.Helper()

	url := os.Getenv("BENCHMARK_NATS_URL")
	if url == "" {
		url = os.Getenv("NATS_URL")
	}
	if url == "" {
		b.Skip("set BENCHMARK_NATS_URL or NATS_URL to run NATS benchmarks")
	}

	contractevents.RegisterTypes(func(e contractevents.Event) { platformevents.RegisterType(e) })
	commands.RegisterTypes(func(e contractevents.Event) { platformevents.RegisterType(e) })

	nc, err := platformnats.NewClient(url, "bench", nil)
	if err != nil {
		b.Fatalf("nats connect: %v", err)
	}

	pub, err := platformnats.NewPublisher(nc, platformnats.PublisherOptions{
		StreamName:    "BENCH_EVENTS",
		SubjectPrefix: "bench",
	})
	if err != nil {
		nc.Close()
		b.Fatalf("nats publisher: %v", err)
	}

	return pub, nc.Close
}

func benchmarkEvent() commands.SendConfirmationEmail {
	return commands.NewSendConfirmationEmail(
		uuid.NewString(),
		uuid.NewString(),
		"bench@example.com",
		"owner/repo",
		"tok-confirm",
		"tok-unsub",
	)
}

// BenchmarkPublish_GRPC measures GRPCPublisher.Publish round-trip latency using
// an in-process gRPC server over a real loopback TCP hop (stub handler, no insert).
// This is the clean transport-only headline number.
func BenchmarkPublish_GRPC(b *testing.B) {
	pub, cleanup := newGRPCPublisher(b)
	defer cleanup()
	cmd := benchmarkEvent()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := pub.Publish(ctx, cmd); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPublish_GRPC_Parallel measures GRPCPublisher throughput under concurrency.
func BenchmarkPublish_GRPC_Parallel(b *testing.B) {
	pub, cleanup := newGRPCPublisher(b)
	defer cleanup()
	cmd := benchmarkEvent()
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := pub.Publish(ctx, cmd); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// BenchmarkPublish_NATS measures the NATS JetStream Publisher.Publish latency.
// Requires BENCHMARK_NATS_URL or NATS_URL; skips otherwise.
func BenchmarkPublish_NATS(b *testing.B) {
	pub, cleanup := newNATSPublisher(b)
	defer cleanup()
	cmd := benchmarkEvent()
	ctx := context.Background()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := pub.Publish(ctx, cmd); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkPublish_NATS_Parallel measures NATS throughput under concurrency.
// Requires BENCHMARK_NATS_URL or NATS_URL; skips otherwise.
func BenchmarkPublish_NATS_Parallel(b *testing.B) {
	pub, cleanup := newNATSPublisher(b)
	defer cleanup()
	cmd := benchmarkEvent()
	ctx := context.Background()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := pub.Publish(ctx, cmd); err != nil {
				b.Fatal(err)
			}
		}
	})
}

// The slow/recovering-consumer collapse scenario is intentionally NOT a testing.B
// benchmark: the publisher semaphore (64) never saturates under RunParallel
// (~GOMAXPROCS goroutines), so an in-proc bench can only measure latency += delay,
// not push collapse. Drive that over the wire with scripts/bench-grpc-slow against
// cmd/notifications-bench (BENCH_SERVICE_TIME set).
