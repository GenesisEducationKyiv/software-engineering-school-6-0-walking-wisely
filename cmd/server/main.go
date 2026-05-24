package main

import (
	"context"
	_ "embed"
	"fmt"
	"log/slog"
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

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/config"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/github"
	githubredis "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/github/redis"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/http/middleware"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail/resend"
	platformlogger "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	platformmetrics "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/metrics"
	platformpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres"
	platformredis "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/redis"
	subscriptiongrpc "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/grpc"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/postgres"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/worker"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
)

//go:embed web/index.html
var indexHTML []byte

func main() {
	appLogger := platformlogger.NewSlogAdapter(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if err := run(appLogger); err != nil {
		appLogger.Error("application startup failed", "err", err)
		os.Exit(1)
	}
}

func run(appLogger platformlogger.Logger) error {
	cfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

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

	if err := postgres.RunMigrations(cfg.DatabaseURL, appLogger); err != nil {
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

	subRepo := postgres.NewSubscriptionRepo(db, appLogger)
	githubClient := github.NewClient(cfg.GithubToken, appLogger)
	releaseCache := githubredis.NewGitHubReleaseCache(redisClient)
	cachedGithubClient := github.NewCachedReleaseClient(githubClient, releaseCache, github.ReleaseCacheTTL, appLogger)
	resendClient := resend.NewClient(cfg.ResendAPIKey, cfg.FromEmail, appLogger)

	emailChan := make(chan mail.Message, cfg.EmailChannelSize)
	if err := metricsRecorder.RegisterEmailChannelDepth(func() int { return len(emailChan) }); err != nil {
		return fmt.Errorf("register email channel depth metric: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		worker.StartScanner(ctx, worker.ScannerDeps{
			Repo:      subRepo,
			GitHub:    cachedGithubClient,
			EmailChan: emailChan,
			BaseURL:   cfg.BaseURL,
			Log:       appLogger,
		}, cfg.ScannerInterval)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		worker.StartSender(ctx, resendClient, emailChan, cfg.ResendMaxWait, appLogger)
	}()

	subService := subscriptiongrpc.NewSubscriptionService(&subscriptiongrpc.ServiceDeps{
		TokenRepo:      subRepo,
		ReadRepo:       subRepo,
		Github:         githubClient,
		EmailChan:      emailChan,
		EmailSecretKey: cfg.EmailSecretKey,
		BaseURL:        cfg.BaseURL,
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
		if err := grpcServer.Serve(lis); err != nil {
			appLogger.Error("gRPC server error", "err", err)
			cancel()
		}
	}()

	// gRPC-Gateway setup (HTTP server)
	gwMux := runtime.NewServeMux(
		runtime.WithMarshalerOption(runtime.MIMEWildcard, &runtime.JSONPb{
			MarshalOptions: protojson.MarshalOptions{
				UseProtoNames:   true,
				EmitUnpopulated: true,
			},
		}),
	)

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

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	err = pb.RegisterSubscribeServiceHandlerFromEndpoint(ctx, gwMux, "localhost:"+grpcPort, opts)
	if err != nil {
		return fmt.Errorf("register gRPC-Gateway: %w", err)
	}

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())
	mux.Handle("/", middleware.Metrics(middleware.Logging(middleware.Recover(gwMux, appLogger), appLogger), metricsRecorder))

	srv := &http.Server{
		Addr:         ":" + cfg.RestPort,
		Handler:      mux,
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
