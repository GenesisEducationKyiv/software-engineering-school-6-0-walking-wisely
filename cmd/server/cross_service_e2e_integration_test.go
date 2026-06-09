//go:build e2e

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	goredis "github.com/redis/go-redis/v9"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	notificationapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/app"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/mail"
	notificationpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/postgres"
	notificationworker "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/worker"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	platformlogger "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/outbox"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/streams"
	releasemonitoringapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/app"
	releasemonitoringdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain"
	releasemonitoringpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/postgres"
	subscriptiongrpc "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/grpc"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/postgres"

	// Register event types so the outbox decoder and stream consumer can decode them.
	_ "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain"
	_ "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app"
)

// ── fake email sender ──────────────────────────────────────────────────────────

type crossServiceFakeSender struct {
	mu   sync.Mutex
	sent []mail.Message
}

func (s *crossServiceFakeSender) MaxBatchSize() int { return 100 }

func (s *crossServiceFakeSender) SendBatch(_ context.Context, msgs []mail.Message) error {
	s.mu.Lock()
	s.sent = append(s.sent, msgs...)
	s.mu.Unlock()
	return nil
}

// waitFor blocks until an email to `to` appears or the timeout elapses.
func (s *crossServiceFakeSender) waitFor(t *testing.T, to string, timeout time.Duration) mail.Message {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		s.mu.Lock()
		for _, e := range s.sent {
			if e.To == to {
				s.mu.Unlock()
				return e
			}
		}
		s.mu.Unlock()
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for email to %q", to)
	return mail.Message{}
}

func (s *crossServiceFakeSender) countFor(to string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	n := 0
	for _, e := range s.sent {
		if e.To == to {
			n++
		}
	}
	return n
}

func (s *crossServiceFakeSender) reset() {
	s.mu.Lock()
	s.sent = nil
	s.mu.Unlock()
}

// ── fake GitHub client ─────────────────────────────────────────────────────────

type crossServiceFakeGitHub struct {
	release *releasemonitoringdomain.Release
}

func (g crossServiceFakeGitHub) ValidateRepo(_ context.Context, _ string) error { return nil }

func (g crossServiceFakeGitHub) GetLatestRelease(_ context.Context, _ string) (*releasemonitoringdomain.Release, error) {
	return g.release, nil
}

// ── Redis container helper ─────────────────────────────────────────────────────

func newCrossServiceRedis(t *testing.T, ctx context.Context) *goredis.Client {
	t.Helper()
	testcontainers.SkipIfProviderIsNotHealthy(t)

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
		t.Fatalf("get redis host: %v", err)
	}
	port, err := container.MappedPort(ctx, "6379/tcp")
	if err != nil {
		t.Fatalf("get redis port: %v", err)
	}

	client := goredis.NewClient(&goredis.Options{Addr: fmt.Sprintf("%s:%s", host, port.Port())})
	t.Cleanup(func() { _ = client.Close() })

	pingCtx, pingCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer pingCancel()
	if err := client.Ping(pingCtx).Err(); err != nil {
		t.Fatalf("ping redis: %v", err)
	}
	return client
}

// ── full stack wiring ──────────────────────────────────────────────────────────

// buildCrossServiceStack wires both microservices against shared Postgres + Redis
// testcontainers. The stream key is passed in to isolate parallel test runs.
// Returns the HTTP test server (API side), fake email sender (notifications side),
// and the scanner service so tests can trigger scans directly.
func buildCrossServiceStack(
	t *testing.T,
	ctx context.Context,
	gh crossServiceFakeGitHub,
	streamKey string,
) (httpServer *httptest.Server, sender *crossServiceFakeSender, scanner *releasemonitoringapp.ScannerService) {
	t.Helper()

	db := newGatewayTestDB(t, ctx)
	redisClient := newCrossServiceRedis(t, ctx)
	const baseURL = "http://app.test"

	// Shared outbox: subscriptions service writes here; dispatcher forwards to Redis.
	outboxRepo := outbox.NewRepository(db)
	outboxPub := outbox.NewPublisher(outboxRepo)
	streamPub := streams.NewPublisher(redisClient, streamKey)

	// API — subscription service uses the transactional outbox as its publisher.
	tokenRepo := postgres.NewTokenRepo(db, platformlogger.NoopLogger{})
	readRepo := postgres.NewReadRepo(db, platformlogger.NoopLogger{})
	releaseScanRepo := releasemonitoringpostgres.NewReleaseScanRepo(db, platformlogger.NoopLogger{})

	subService := subscriptiongrpc.NewSubscriptionService(&subscriptiongrpc.ServiceDeps{
		TokenRepo:      tokenRepo,
		TxManager:      tokenRepo,
		ReadRepo:       readRepo,
		Github:         gh,
		Publisher:      outboxPub,
		EmailSecretKey: "e2e-test-secret",
		Log:            platformlogger.NoopLogger{},
	})

	scannerSvc := releasemonitoringapp.NewScannerService(releasemonitoringapp.ScannerDeps{
		Repo:      releaseScanRepo,
		GitHub:    gh,
		TxManager: releaseScanRepo,
		Publisher: outboxPub,
		Log:       platformlogger.NoopLogger{},
	})

	// Outbox dispatcher runs continuously, forwarding DB records → Redis stream.
	go outbox.StartDispatcher(ctx, outboxRepo, streamPub, 50*time.Millisecond, 32, 5, platformlogger.NoopLogger{})

	// Wire gRPC + HTTP gateway.
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen gRPC: %v", err)
	}
	grpcSrv := grpc.NewServer()
	pb.RegisterSubscribeServiceServer(grpcSrv, subService)
	t.Cleanup(grpcSrv.Stop)
	go func() { _ = grpcSrv.Serve(lis) }()

	gwMux := newGatewayMux()
	if err := registerGatewayRoutes(gwMux); err != nil {
		t.Fatalf("register gateway routes: %v", err)
	}
	if err := pb.RegisterSubscribeServiceHandlerFromEndpoint(
		ctx, gwMux, lis.Addr().String(),
		[]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	); err != nil {
		t.Fatalf("register gateway handler: %v", err)
	}
	srv := httptest.NewServer(newHTTPHandler(gwMux, http.NotFoundHandler(), gatewayTestMetricsRecorder{}, platformlogger.NoopLogger{}))
	t.Cleanup(srv.Close)

	// Notifications service — reads from Redis stream, records jobs, sends emails.
	fakeSender := &crossServiceFakeSender{}
	notifJobRepo := notificationpostgres.NewRepository(db)
	bus := events.NewBus()
	notificationapp.NewEventHandlers(notifJobRepo, baseURL, platformlogger.NoopLogger{}).Register(bus)

	consumer := streams.NewConsumer(redisClient, streamKey, "notifications", uuid.NewString(), 32, platformlogger.NoopLogger{})
	go func() { _ = consumer.Run(ctx, bus) }()

	go notificationworker.StartSender(ctx, fakeSender, notifJobRepo, 50*time.Millisecond, platformlogger.NoopLogger{})

	return srv, fakeSender, scannerSvc
}

// ── Tests ─────────────────────────────────────────────────────────────────────

// TestE2E_CrossService_SubscriptionConfirmationEmailDelivered verifies
// that a POST /api/subscribe causes a confirmation email to land in the sender
// after crossing the outbox → Redis stream → notifications consumer boundary.
func TestE2E_CrossService_SubscriptionConfirmationEmailDelivered(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	srv, sender, _ := buildCrossServiceStack(t, ctx, crossServiceFakeGitHub{}, "e2e-confirm-"+uuid.NewString()[:8])

	postJSON(t, srv.URL+"/api/subscribe", map[string]string{
		"email": "alice@example.com",
		"repo":  "owner/repo",
	}, http.StatusOK)

	email := sender.waitFor(t, "alice@example.com", 30*time.Second)

	if !strings.Contains(email.Subject, "owner/repo") {
		t.Errorf("subject %q missing repo name", email.Subject)
	}
	if !strings.Contains(email.HTML, "/api/confirm/") {
		t.Errorf("email HTML missing confirm link")
	}
	if !strings.Contains(email.HTML, "/api/unsubscribe/") {
		t.Errorf("email HTML missing unsubscribe link")
	}
}

// TestE2E_CrossService_ConfirmAndReceiveReleaseNotification covers:
//  1. Subscribe → confirmation email delivered end-to-end.
//  2. Confirm subscription via the API.
//  3. Scanner detects new release → outbox → Redis stream → notification job → email sent.
func TestE2E_CrossService_ConfirmAndReceiveReleaseNotification(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	gh := crossServiceFakeGitHub{release: &releasemonitoringdomain.Release{
		TagName: "v1.0.0",
		Name:    "First Release",
		HTMLURL: "https://github.com/owner/repo/releases/tag/v1.0.0",
	}}

	srv, sender, scannerSvc := buildCrossServiceStack(t, ctx, gh, "e2e-release-"+uuid.NewString()[:8])

	// Step 1 – Subscribe.
	postJSON(t, srv.URL+"/api/subscribe", map[string]string{
		"email": "bob@example.com",
		"repo":  "owner/repo",
	}, http.StatusOK)

	// Step 2 – Confirm.
	confirmEmail := sender.waitFor(t, "bob@example.com", 30*time.Second)
	confirmToken := extractGatewayTestToken(t, confirmEmail.HTML, "/api/confirm/")
	getStatus(t, srv.URL+"/api/confirm/"+confirmToken, http.StatusOK)

	var listed []gatewaySubscription
	getJSON(t, srv.URL+"/api/subscriptions?email="+url.QueryEscape("bob@example.com"), http.StatusOK, &listed)
	if len(listed) != 1 || !listed[0].Confirmed {
		t.Fatalf("want 1 confirmed subscription, got %+v", listed)
	}

	// Step 3 – Trigger scanner; watch for release notification email.
	sender.reset()
	scannerSvc.Scan(ctx)

	releaseEmail := sender.waitFor(t, "bob@example.com", 30*time.Second)

	if !strings.Contains(releaseEmail.Subject, "v1.0.0") {
		t.Errorf("subject %q missing tag", releaseEmail.Subject)
	}
	if !strings.Contains(releaseEmail.Subject, "owner/repo") {
		t.Errorf("subject %q missing repo", releaseEmail.Subject)
	}
	if !strings.Contains(releaseEmail.HTML, "First Release") {
		t.Errorf("HTML missing release name")
	}
	if !strings.Contains(releaseEmail.HTML, "https://github.com/owner/repo/releases/tag/v1.0.0") {
		t.Errorf("HTML missing GitHub release URL")
	}
	if !strings.Contains(releaseEmail.HTML, "/api/unsubscribe/") {
		t.Errorf("HTML missing unsubscribe link")
	}
}

// TestE2E_CrossService_ScannerDeduplicatesAlreadySeenRelease verifies
// a second scan for the same tag does not produce another email.
func TestE2E_CrossService_ScannerDeduplicatesAlreadySeenRelease(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	gh := crossServiceFakeGitHub{release: &releasemonitoringdomain.Release{
		TagName: "v2.0.0",
		HTMLURL: "https://github.com/owner/repo/releases/tag/v2.0.0",
	}}

	srv, sender, scannerSvc := buildCrossServiceStack(t, ctx, gh, "e2e-dedup-"+uuid.NewString()[:8])

	postJSON(t, srv.URL+"/api/subscribe", map[string]string{"email": "carol@example.com", "repo": "owner/repo"}, http.StatusOK)
	confirmEmail := sender.waitFor(t, "carol@example.com", 30*time.Second)
	confirmToken := extractGatewayTestToken(t, confirmEmail.HTML, "/api/confirm/")
	getStatus(t, srv.URL+"/api/confirm/"+confirmToken, http.StatusOK)

	// First scan — release notification lands.
	sender.reset()
	scannerSvc.Scan(ctx)
	sender.waitFor(t, "carol@example.com", 30*time.Second)

	// Second scan with the same tag — must not produce another email.
	beforeCount := sender.countFor("carol@example.com")
	scannerSvc.Scan(ctx)
	time.Sleep(2 * time.Second) // allow pipeline time to process anything unexpected
	afterCount := sender.countFor("carol@example.com")

	if afterCount != beforeCount {
		t.Errorf("second scan produced %d extra emails, want 0", afterCount-beforeCount)
	}
}

// TestE2E_CrossService_ResubscribeAfterConfirmationIsRejected checks that a
// second subscribe for an already-confirmed subscription returns HTTP 409 and
// does not produce another email end-to-end.
func TestE2E_CrossService_ResubscribeAfterConfirmationIsRejected(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	srv, sender, _ := buildCrossServiceStack(t, ctx, crossServiceFakeGitHub{}, "e2e-idem-"+uuid.NewString()[:8])

	// Subscribe and confirm.
	postJSON(t, srv.URL+"/api/subscribe", map[string]string{"email": "dave@example.com", "repo": "owner/repo"}, http.StatusOK)
	confirmEmail := sender.waitFor(t, "dave@example.com", 30*time.Second)
	confirmToken := extractGatewayTestToken(t, confirmEmail.HTML, "/api/confirm/")
	getStatus(t, srv.URL+"/api/confirm/"+confirmToken, http.StatusOK)

	// Subscribing again once confirmed must be rejected with 409.
	postJSON(t, srv.URL+"/api/subscribe", map[string]string{"email": "dave@example.com", "repo": "owner/repo"}, http.StatusConflict)

	// No additional emails should have been queued.
	time.Sleep(2 * time.Second)
	if n := sender.countFor("dave@example.com"); n != 1 {
		t.Errorf("emails sent = %d, want exactly 1 (no extra after confirmed re-subscribe)", n)
	}
}
