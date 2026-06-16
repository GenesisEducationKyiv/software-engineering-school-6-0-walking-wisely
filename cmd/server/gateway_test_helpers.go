//go:build integration || e2e

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
	platformlogger "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	subscriptiongrpc "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/grpc"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/postgres"
)

type gatewaySubscription struct {
	Email       string `json:"email"`
	Repo        string `json:"repo"`
	Confirmed   bool   `json:"confirmed"`
	LastSeenTag string `json:"last_seen_tag"`
}

type gatewayTestGitHub struct{}

func (gatewayTestGitHub) ValidateRepo(context.Context, string) error {
	return nil
}

type gatewayTestMetricsRecorder struct{}

func (gatewayTestMetricsRecorder) RecordHTTPRequest(context.Context, string, string, int, time.Duration) {
}

func (gatewayTestMetricsRecorder) RegisterEmailChannelDepth(func() int) error {
	return nil
}

func (gatewayTestMetricsRecorder) RegisterGitHubAvailability(func() bool) error {
	return nil
}

func (gatewayTestMetricsRecorder) RegisterGitHubRateLimitRemaining(func() int) error {
	return nil
}

func newGatewayTestDB(t *testing.T, ctx context.Context) *pgxpool.Pool {
	t.Helper()

	testcontainers.SkipIfProviderIsNotHealthy(t)

	container, err := tcpostgres.Run(
		ctx,
		"postgres:16-alpine",
		tcpostgres.WithDatabase("app"),
		tcpostgres.WithUsername("app"),
		tcpostgres.WithPassword("secret"),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Fatalf("start postgres container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate postgres container: %v", err)
		}
	})

	databaseURL, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("build postgres connection string: %v", err)
	}
	if err := postgres.RunMigrations(databaseURL, platformlogger.NoopLogger{}); err != nil {
		t.Fatalf("run migrations: %v", err)
	}

	db, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatalf("connect to postgres: %v", err)
	}
	t.Cleanup(db.Close)

	return db
}

func newGatewayTestServer(
	t *testing.T,
	ctx context.Context,
	db *pgxpool.Pool,
	emailChan chan mail.Message,
	baseURL string,
) *httptest.Server {
	t.Helper()

	tokenRepo := postgres.NewTokenRepo(db, platformlogger.NoopLogger{})
	readRepo := postgres.NewReadRepo(db, platformlogger.NoopLogger{})
	service := subscriptiongrpc.NewSubscriptionService(&subscriptiongrpc.ServiceDeps{
		TokenRepo:      tokenRepo,
		ReadRepo:       readRepo,
		Github:         gatewayTestGitHub{},
		EmailChan:      emailChan,
		EmailSecretKey: "test-secret",
		BaseURL:        baseURL,
		Log:            platformlogger.NoopLogger{},
	})

	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen for gRPC server: %v", err)
	}

	grpcServer := grpc.NewServer()
	pb.RegisterSubscribeServiceServer(grpcServer, service)
	t.Cleanup(grpcServer.Stop)

	serveErr := make(chan error, 1)
	go func() {
		serveErr <- grpcServer.Serve(lis)
	}()
	t.Cleanup(func() {
		select {
		case err := <-serveErr:
			if err != nil {
				t.Logf("gRPC server stopped: %v", err)
			}
		default:
		}
	})

	gwMux := newGatewayMux()
	if err := registerGatewayRoutes(gwMux); err != nil {
		t.Fatalf("register gateway routes: %v", err)
	}
	if err := pb.RegisterSubscribeServiceHandlerFromEndpoint(
		ctx,
		gwMux,
		lis.Addr().String(),
		[]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())},
	); err != nil {
		t.Fatalf("register gateway handler: %v", err)
	}

	server := httptest.NewServer(newHTTPHandler(
		gwMux,
		http.NotFoundHandler(),
		gatewayTestMetricsRecorder{},
		platformlogger.NoopLogger{},
	))
	t.Cleanup(server.Close)

	return server
}

func postJSON(t *testing.T, target string, body any, wantStatus int) {
	t.Helper()

	payload, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}
	req, err := http.NewRequest(http.MethodPost, target, bytes.NewReader(payload))
	if err != nil {
		t.Fatalf("create POST request: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST %s: %v", target, err)
	}
	defer resp.Body.Close()

	assertGatewayStatus(t, resp, wantStatus)
}

func getJSON(t *testing.T, target string, wantStatus int, dst any) {
	t.Helper()

	resp, err := http.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()

	assertGatewayStatus(t, resp, wantStatus)
	if err := json.NewDecoder(resp.Body).Decode(dst); err != nil {
		t.Fatalf("decode response body: %v", err)
	}
}

func getStatus(t *testing.T, target string, wantStatus int) {
	t.Helper()

	resp, err := http.Get(target)
	if err != nil {
		t.Fatalf("GET %s: %v", target, err)
	}
	defer resp.Body.Close()

	assertGatewayStatus(t, resp, wantStatus)
}

func assertGatewayStatus(t *testing.T, resp *http.Response, wantStatus int) {
	t.Helper()

	if resp.StatusCode == wantStatus {
		return
	}
	body, _ := io.ReadAll(resp.Body)
	t.Fatalf("status = %d, want %d, body: %s", resp.StatusCode, wantStatus, string(body))
}

func receiveGatewayTestEmail(t *testing.T, ch <-chan mail.Message) mail.Message {
	t.Helper()

	select {
	case msg := <-ch:
		return msg
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for confirmation email")
		return mail.Message{}
	}
}

func extractGatewayTestToken(t *testing.T, html, routePrefix string) string {
	t.Helper()

	re := regexp.MustCompile(regexp.QuoteMeta(routePrefix) + `([a-f0-9]{64})`)
	matches := re.FindStringSubmatch(html)
	if len(matches) != 2 {
		t.Fatalf("email HTML missing token for %s: %s", routePrefix, html)
	}
	return matches[1]
}
