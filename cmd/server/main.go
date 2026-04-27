package main

import (
	"context"
	_ "embed"
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
	"github.com/walking-wisely/genesis2026-github-release-api/internal/clients"
	"github.com/walking-wisely/genesis2026-github-release-api/internal/config"
	"github.com/walking-wisely/genesis2026-github-release-api/internal/domain"
	"github.com/walking-wisely/genesis2026-github-release-api/internal/http/handlers"
	"github.com/walking-wisely/genesis2026-github-release-api/internal/http/middleware"
	"github.com/walking-wisely/genesis2026-github-release-api/internal/repository"
	"github.com/walking-wisely/genesis2026-github-release-api/internal/workers"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/reflection"
	"google.golang.org/protobuf/encoding/protojson"

	pb "github.com/walking-wisely/genesis2026-github-release-api/gen/subscription/v1"
)

//go:embed web/index.html
var indexHTML []byte

func main() {
	// JSON structured logging to stdout from the very first line.
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	cfg, err := config.LoadAppConfig()
	if err != nil {
		slog.Error("load config", "err", err)
		os.Exit(1)
	}

	if err := repository.RunMigrations(cfg.DatabaseURL); err != nil {
		slog.Error("run migrations", "err", err)
		os.Exit(1)
	}

	db, err := config.InitDB(cfg.DatabaseURL)
	if err != nil {
		slog.Error("init database", "err", err)
		os.Exit(1)
	}
	defer db.Close()

	redisClient, err := config.InitRedis(cfg.RedisURL)
	if err != nil {
		slog.Error("init redis", "err", err)
		os.Exit(1)
	}
	defer redisClient.Close()

	subRepo := repository.NewSubscriptionRepo(db)
	githubClient := clients.NewGitHubClient(redisClient, cfg.GithubToken)
	resendClient := clients.NewResendClient(cfg.ResendAPIKey, cfg.FromEmail)

	emailChan := make(chan domain.EmailMessage, cfg.EmailChannelSize)

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
		workers.StartScanner(ctx, workers.ScannerDeps{
			Repo:      subRepo,
			GitHub:    githubClient,
			EmailChan: emailChan,
			BaseURL:   cfg.BaseURL,
		}, cfg.ScannerInterval)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		workers.StartSender(ctx, resendClient, emailChan, cfg.ResendMaxWait)
	}()

	subService := handlers.NewSubscriptionService(handlers.ServiceDeps{
		SubRepo:        subRepo,
		Github:         githubClient,
		EmailChan:      emailChan,
		EmailSecretKey: cfg.EmailSecretKey,
		BaseUrl:        cfg.BaseURL,
	})

	grpcServer := grpc.NewServer()
	pb.RegisterSubscribeServiceServer(grpcServer, subService)
	reflection.Register(grpcServer)

	grpcPort := cfg.GrpcPort
	lis, err := net.Listen("tcp", ":"+grpcPort)
	if err != nil {
		slog.Error("grpc server listen", "err", err)
		os.Exit(1)
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

	gwMux.HandlePath("GET", "/swagger.json", func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
		http.ServeFile(w, r, "gen/subscription/v1/subscription.swagger.json")
	})

	gwMux.HandlePath("GET", "/health", func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	gwMux.HandlePath("GET", "/", func(w http.ResponseWriter, r *http.Request, _ map[string]string) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})

	opts := []grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}
	err = pb.RegisterSubscribeServiceHandlerFromEndpoint(ctx, gwMux, "localhost:"+grpcPort, opts)
	if err != nil {
		slog.Error("failed to register gRPC-Gateway", "err", err)
		os.Exit(1)
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
}
