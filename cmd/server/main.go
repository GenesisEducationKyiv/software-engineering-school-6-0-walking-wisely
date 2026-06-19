package main

import (
	"context"
	_ "embed"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/encoding/protojson"

	contractcommands "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/commands"
	contractevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/integrations/github"
	githubredis "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/integrations/github/redis"
	platformconfig "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/config"
	platformevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/http/middleware"
	platformlogger "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	platformmetrics "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/metrics"
	platformnats "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/nats"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/outbox"
	platformpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres"
	platformmigrations "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres/migrations"
	platformredis "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/redis"
	releasemonitoringapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/app"
	releasemonitoringpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/postgres"
	releasemonitoringworker "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/worker"
	subscriptionapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app"
	subscriptiongrpc "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/grpc"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/postgres"

	// Register event types so outbox can decode them for JetStream publishing.
	_ "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
)

//go:embed web/index.html
var indexHTML []byte

func main() {
	appLogger := platformlogger.NewStructured(os.Stdout, platformlogger.StructuredConfig{})

	if err := run(appLogger); err != nil {
		appLogger.Error("application startup failed", "err", err)
		os.Exit(1)
	}
}

func run(appLogger platformlogger.Logger) error {
	contractevents.RegisterTypes(func(event contractevents.Event) {
		platformevents.RegisterType(event)
	})
	contractcommands.RegisterTypes(func(event contractevents.Event) {
		platformevents.RegisterType(event)
	})

	cfg, err := platformconfig.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	appLogger = platformlogger.NewStructured(os.Stdout, platformlogger.StructuredConfig{
		Level:       cfg.LogLevel,
		ServiceName: cfg.ServiceName,
		Environment: cfg.Environment,
	})

	meterProvider, err := platformmetrics.InitMeterProvider()
	if err != nil {
		return fmt.Errorf("init meter provider: %w", err)
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		if err := meterProvider.Shutdown(shutdownCtx); err != nil {
			appLogger.Error("shutdown meter provider", "err", err)
		}
	}()
	metricsRecorder, err := middleware.NewOpenTelemetryRecorder(
		meterProvider.Meter("github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/cmd/server"),
	)
	if err != nil {
		return fmt.Errorf("init metrics recorder: %w", err)
	}

	if err := platformmigrations.Run(cfg.DatabaseURL, appLogger); err != nil {
		return fmt.Errorf("run database migrations: %w", err)
	}

	db, err := platformpostgres.NewDB(cfg.DatabaseURL, appLogger)
	if err != nil {
		return fmt.Errorf("init database: %w", err)
	}
	defer db.Close()

	redisClient, err := platformredis.NewClient(cfg.RedisURL, appLogger)
	if err != nil {
		return fmt.Errorf("init redis: %w", err)
	}

	defer func() {
		if err := redisClient.Close(); err != nil {
			appLogger.Error("close redis", "err", err)
		}
	}()

	natsClient, err := platformnats.NewClient(cfg.NATSURL, cfg.ServiceName, appLogger)
	if err != nil {
		return fmt.Errorf("init nats: %w", err)
	}
	defer natsClient.Close()

	subTokenRepo := postgres.NewTokenRepo(db, appLogger)
	subReadRepo := postgres.NewReadRepo(db, appLogger)
	subSagaRepo := postgres.NewSagaRepository(db)
	releaseScanRepo := releasemonitoringpostgres.NewReleaseScanRepo(db, appLogger)
	outboxRepo := outbox.NewRepository(db)
	outboxPublisher := outbox.NewPublisher(outboxRepo)
	githubClient := github.NewClient(cfg.GithubToken, appLogger)
	githubAvailability := github.NewAvailabilityState()
	checkCtx, checkCancel := context.WithTimeout(context.Background(), 5*time.Second)
	github.UpdateAvailability(checkCtx, githubClient, githubAvailability, appLogger)
	checkCancel()
	releaseCache := githubredis.NewGitHubReleaseCache(redisClient)
	cachedGithubClient := github.NewCachedReleaseClient(githubClient, releaseCache, github.ReleaseCacheTTL, appLogger)

	eventPublisher, err := platformnats.NewPublisher(natsClient, platformnats.PublisherOptions{
		StreamName:    cfg.NATSStreamName,
		SubjectPrefix: cfg.NATSSubjectPrefix,
	})
	if err != nil {
		return fmt.Errorf("init jetstream publisher: %w", err)
	}

	if err := metricsRecorder.RegisterOutboxMetrics(func(ctx context.Context) (int64, float64, int64, int64, error) {
		snapshot, err := outboxRepo.Metrics(ctx)
		if err != nil {
			return 0, 0, 0, 0, err
		}
		return snapshot.PendingCount, snapshot.OldestPendingAge, snapshot.RetryCount, snapshot.FailedCount, nil
	}); err != nil {
		return fmt.Errorf("register outbox metrics: %w", err)
	}
	if err := metricsRecorder.RegisterGitHubAvailability(githubAvailability.Available); err != nil {
		return fmt.Errorf("register github availability metric: %w", err)
	}
	if err := metricsRecorder.RegisterGitHubRateLimitRemaining(githubAvailability.RateLimitRemaining); err != nil {
		return fmt.Errorf("register github rate limit remaining metric: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	scannerService := releasemonitoringapp.NewScannerService(&releasemonitoringapp.ScannerDeps{
		Repo:      releaseScanRepo,
		GitHub:    cachedGithubClient,
		TxManager: releaseScanRepo,
		Publisher: outboxPublisher,
		Log:       appLogger,
	})

	wg.Add(1)
	go func() {
		defer wg.Done()
		releasemonitoringworker.StartScanner(ctx, scannerService, cfg.ScannerInterval, appLogger)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		github.StartAvailabilityMonitor(
			ctx,
			githubClient,
			githubAvailability,
			appLogger,
			github.DefaultAvailabilityCheckInterval,
		)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		outbox.StartDispatcher(ctx, outboxRepo, eventPublisher, 200*time.Millisecond, 32, 5, appLogger)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		outbox.StartCleanup(ctx, outboxRepo, cfg.OutboxCleanupInterval, cfg.OutboxRetention, appLogger)
	}()

	// Saga orchestrator — drives the subscribe confirmation saga.
	sagaOrchestrator := subscriptionapp.NewSagaOrchestrator(&subscriptionapp.SagaOrchestratorDeps{
		SagaRepo:  subSagaRepo,
		SubRepo:   subTokenRepo,
		TxManager: subTokenRepo,
		Publisher: outboxPublisher,
		Log:       appLogger,
	})

	// Reply consumer — receives ConfirmationEmailSent / ConfirmationEmailFailed from NATS.
	replyBus := platformevents.NewBus()
	sagaOrchestrator.RegisterReplyHandlers(replyBus)

	replyConsumer, err := platformnats.NewConsumer(natsClient, &platformnats.ConsumerOptions{
		StreamName:    cfg.NATSStreamName,
		SubjectPrefix: cfg.NATSSubjectPrefix,
		ConsumerName:  cfg.NATSConsumerName,
		BatchSize:     cfg.NATSBatchSize,
		AckWait:       cfg.NATSAckWait,
		MaxDeliveries: cfg.NATSMaxDeliveries,
		DLQSubject:    cfg.NATSDLQSubject,
	}, appLogger)
	if err != nil {
		return fmt.Errorf("init saga reply consumer: %w", err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := replyConsumer.Run(ctx, replyBus); err != nil {
			appLogger.Error("saga reply consumer error", "err", err)
			cancel()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(cfg.SagaSweepInterval)
		defer ticker.Stop()
		sagaOrchestrator.Sweep(ctx, cfg.SagaStuckAfter)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				sagaOrchestrator.Sweep(ctx, cfg.SagaStuckAfter)
			}
		}
	}()

	subService := subscriptiongrpc.NewSubscriptionService(&subscriptiongrpc.ServiceDeps{
		TokenRepo:      subTokenRepo,
		TxManager:      subTokenRepo,
		ReadRepo:       subReadRepo,
		Github:         githubClient,
		Orchestrator:   sagaOrchestrator,
		EmailSecretKey: cfg.EmailSecretKey,
		Log:            appLogger,
	})

	grpcServer := grpc.NewServer()
	pb.RegisterSubscribeServiceServer(grpcServer, subService)
	reflection.Register(grpcServer)

	grpcPort := cfg.GrpcPort
	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		return fmt.Errorf("listen on gRPC port: %w", err)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		appLogger.Info("gRPC server listening", "port", grpcPort)
		if err := grpcServer.Serve(lis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			appLogger.Error("gRPC server error", "err", err)
			cancel()
		}
	}()

	gwMux := newGatewayMux()
	if err := registerGatewayRoutes(gwMux); err != nil {
		return err
	}

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	err = pb.RegisterSubscribeServiceHandlerFromEndpoint(ctx, gwMux, "localhost:"+grpcPort, opts)
	if err != nil {
		return fmt.Errorf("register gRPC-Gateway: %w", err)
	}

	srv := &http.Server{
		Addr:         ":" + cfg.RestPort,
		Handler:      newHTTPHandler(gwMux, promhttp.Handler(), metricsRecorder, appLogger),
		ReadTimeout:  10 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		appLogger.Info("HTTP REST gateway listening", "port", cfg.RestPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			appLogger.Error("http server error", "err", err)
			cancel()
		}
	}()

	// Block until SIGINT or SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	appLogger.Info("shutdown signal received")
	cancel()

	grpcServer.GracefulStop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		appLogger.Error("http server shutdown", "err", err)
	}

	wg.Wait()
	appLogger.Info("shutdown complete")
	return nil
}

func newGatewayMux() *runtime.ServeMux {
	return runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames:   true,
				EmitUnpopulated: true,
			},
		}),
	)
}

func registerGatewayRoutes(gwMux *runtime.ServeMux) error {
	if err := gwMux.HandlePath("GET", "/swagger.json", func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
		http.ServeFile(w, r, "gen/subscription/v1/subscription.swagger.json")
	}); err != nil {
		return fmt.Errorf("register route GET /swagger.json: %w", err)
	}

	if err := gwMux.HandlePath("GET", "/health", func(w http.ResponseWriter, _ *http.Request, _ map[string]string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}); err != nil {
		return fmt.Errorf("register route GET /health: %w", err)
	}

	if err := gwMux.HandlePath("GET", "/", func(w http.ResponseWriter, _ *http.Request, _ map[string]string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	}); err != nil {
		return fmt.Errorf("register route GET /: %w", err)
	}

	return nil
}

func newHTTPHandler(
	gwMux http.Handler,
	metricsHandler http.Handler,
	recorder middleware.MetricsRecorder,
	log platformlogger.Logger,
) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/metrics", metricsHandler)
	mux.Handle("/", middleware.Metrics(
		middleware.Logging(
			middleware.Recover(gwMux, log),
			log,
		),
		recorder,
	))
	return mux
}
