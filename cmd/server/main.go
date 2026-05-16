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
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/encoding/protojson"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/config"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/github"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/http/middleware"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail/resend"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
	subscriptiongrpc "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/grpc"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/postgres"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/worker"

	pb "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/subscription/v1"
)

//go:embed web/index.html
var indexHTML []byte

func main() {
	if err := run(); err != nil {
		slog.Error("application startup failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	// JSON structured logging to stdout from the very first line.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := config.LoadAppConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if err := postgres.RunMigrations(cfg.DatabaseURL); err != nil {
		return fmt.Errorf("run database migrations: %w", err)
	}

	db, err := config.InitDB(cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("init database: %w", err)
	}
	defer db.Close()

	redisClient, err := config.InitRedis(cfg.RedisURL)
	if err != nil {
		return fmt.Errorf("init redis: %w", err)
	}

	defer func() {
		if err := redisClient.Close(); err != nil {
			slog.Error("close redis", "err", err)
		}
	}()

	subRepo := postgres.NewSubscriptionRepo(db)
	githubClient := github.NewGitHubClient(redisClient, cfg.GithubToken)
	resendClient := resend.NewResendClient(cfg.ResendAPIKey, cfg.FromEmail)

	emailChan := make(chan subscriptions.EmailMessage, cfg.EmailChannelSize)

	promauto.NewGaugeFunc(prometheus.GaugeOpts{
		Name: "email_channel_depth",
		Help: "Number of pending emails in the send queue.",
	}, func() float64 { return float64(len(emailChan)) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		worker.StartScanner(ctx, worker.ScannerDeps{
			Repo:      subRepo,
			GitHub:    githubClient,
			EmailChan: emailChan,
			BaseURL:   cfg.BaseURL,
		}, cfg.ScannerInterval)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		worker.StartSender(ctx, resendClient, emailChan, cfg.ResendMaxWait)
	}()

	subService := subscriptiongrpc.NewSubscriptionService(subscriptiongrpc.ServiceDeps{
		TokenRepo:      subRepo,
		ReadRepo:       subRepo,
		Github:         githubClient,
		EmailChan:      emailChan,
		EmailSecretKey: cfg.EmailSecretKey,
		BaseURL:        cfg.BaseURL,
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
		slog.Info("gRPC server listening", "port", grpcPort)
		if err := grpcServer.Serve(lis); err != nil {
			slog.Error("gRPC server error", "err", err)
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
	mux.Handle("/", middleware.Metrics(middleware.Logging(middleware.Recover(gwMux))))

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
		slog.Info("HTTP REST gateway listening", "port", cfg.RestPort)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "err", err)
			cancel()
		}
	}()

	// Block until SIGINT or SIGTERM.
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	slog.Info("shutdown signal received")
	cancel()

	grpcServer.GracefulStop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer shutdownCancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		slog.Error("http server shutdown", "err", err)
	}

	wg.Wait()
	slog.Info("shutdown complete")
	return nil
}
