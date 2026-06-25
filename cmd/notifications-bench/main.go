//go:build bench

// cmd/notifications-bench is a bench-only binary: identical to cmd/notifications
// but registers a SlowServer decorator that injects synthetic service-time into
// SendConfirmation. Compiled only with -tags bench — never shipped to production.
//
// Usage:
//
//	go build -tags bench ./cmd/notifications-bench
//	BENCH_SERVICE_TIME=5ms ./notifications-bench
package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	notificationv1 "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/notification/v1"
	contractcommands "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/commands"
	contractevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/integrations/resend"
	notificationapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/app"
	notificationgrpc "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/grpc"
	notificationpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/postgres"
	notificationworker "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/worker"
	platformconfig "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/config"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	platformlogger "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	platformnats "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/nats"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/outbox"
	platformpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres"
)

func main() {
	log := platformlogger.NewStructured(os.Stdout, platformlogger.StructuredConfig{})
	if err := run(log); err != nil {
		log.Error("notifications-bench startup failed", "err", err)
		os.Exit(1)
	}
}

// benchSubscriptionID is a fixed, well-known subscription row seeded at startup.
// notification_jobs.subscription_id has a FK to subscriptions(id), so the ghz
// load scripts send this constant as subscription_id to satisfy the constraint
// while event_id/confirm_token still vary per request for real inserts.
const benchSubscriptionID = "11111111-1111-1111-1111-111111111111"

// seedBenchSubscription inserts the fixed bench subscription if absent so the
// SendConfirmation handler's job insert does not violate the FK. Idempotent.
func seedBenchSubscription(ctx context.Context, db *pgxpool.Pool) error {
	_, err := db.Exec(
		ctx,
		`INSERT INTO subscriptions (id, email, repo, confirmed, confirm_token, unsubscribe_token)
		 VALUES ($1::uuid, 'bench@example.com', 'owner/repo', true, 'bench-confirm-token', 'bench-unsub-token')
		 ON CONFLICT (id) DO NOTHING`,
		benchSubscriptionID,
	)
	if err != nil {
		return err
	}
	return nil
}

func serviceDelay(log platformlogger.Logger) time.Duration {
	raw := os.Getenv("BENCH_SERVICE_TIME")
	if raw == "" {
		return 0
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		log.Error("BENCH_SERVICE_TIME: invalid duration", "value", raw, "err", err)
		os.Exit(1)
	}
	return d
}

func run(log platformlogger.Logger) error {
	contractevents.RegisterTypes(func(event contractevents.Event) {
		events.RegisterType(event)
	})
	contractcommands.RegisterTypes(func(event contractevents.Event) {
		events.RegisterType(event)
	})

	cfg, err := platformconfig.LoadNotificationsConfig()
	if err != nil {
		return err
	}

	log = platformlogger.NewStructured(os.Stdout, platformlogger.StructuredConfig{
		Level:       cfg.LogLevel,
		ServiceName: cfg.ServiceName + "-bench",
		Environment: cfg.Environment,
	})

	delay := serviceDelay(log)
	if delay > 0 {
		log.Info("bench mode: synthetic service-time active", "delay", delay)
	} else {
		log.Info("bench mode: synthetic service-time disabled (BENCH_SERVICE_TIME not set)")
	}

	db, err := platformpostgres.NewDB(cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := seedBenchSubscription(context.Background(), db); err != nil {
		return err
	}
	log.Info("bench mode: seeded subscription", "subscription_id", benchSubscriptionID)

	natsClient, err := platformnats.NewClient(cfg.NATSURL, cfg.ServiceName, log)
	if err != nil {
		return err
	}
	defer natsClient.Close()

	notificationsOutboxRepo := outbox.NewRepository(db, "notifications_outbox")
	notificationsOutboxPublisher, err := platformnats.NewPublisher(natsClient, platformnats.PublisherOptions{
		StreamName:    cfg.NATSStreamName,
		SubjectPrefix: cfg.NATSSubjectPrefix,
	})
	if err != nil {
		return err
	}

	notificationJobRepo := notificationpostgres.NewRepository(db, notificationsOutboxRepo, cfg.JobInsertBatchSize)

	var emailSender mail.Sender
	if cfg.EmailSink == "noop" {
		emailSender = mail.NoopSender{}
	} else {
		emailSender = resend.NewClient(cfg.ResendAPIKey, cfg.FromEmail, log)
	}

	bus := events.NewBus()
	notificationHandlers := notificationapp.NewEventHandlers(notificationJobRepo, cfg.BaseURL, log)
	notificationHandlers.Register(bus)

	consumer, err := platformnats.NewConsumer(natsClient, &platformnats.ConsumerOptions{
		StreamName:    cfg.NATSStreamName,
		SubjectPrefix: cfg.NATSSubjectPrefix,
		ConsumerName:  cfg.NATSConsumerName,
		BatchSize:     cfg.NATSBatchSize,
		AckWait:       cfg.NATSAckWait,
		MaxDeliveries: cfg.NATSMaxDeliveries,
		DLQSubject:    cfg.NATSDLQSubject,
	}, log)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		if err := consumer.Run(ctx, bus); err != nil {
			log.Error("stream consumer error", "err", err)
			cancel()
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		notificationworker.StartSender(ctx, emailSender, notificationJobRepo, cfg.ResendMaxWait, log)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		notificationworker.StartCleanup(ctx, notificationJobRepo, cfg.JobCleanupInterval, cfg.JobRetention, log)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		outbox.StartDispatcher(ctx, notificationsOutboxRepo, notificationsOutboxPublisher, cfg.NotificationsOutboxInterval, 32, 5, log)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		outbox.StartCleanup(ctx, notificationsOutboxRepo, cfg.NotificationsOutboxInterval, cfg.NotificationsOutboxRetention, log)
	}()

	// Register the slow-handler decorator (bench tag only — excluded from prod build).
	inner := notificationgrpc.NewServer(notificationHandlers)
	var handler notificationv1.NotificationServiceServer
	if delay > 0 {
		handler = notificationgrpc.NewSlowServer(inner, delay)
	} else {
		handler = inner
	}

	grpcSrv := grpc.NewServer()
	notificationv1.RegisterNotificationServiceServer(grpcSrv, handler)
	reflection.Register(grpcSrv)

	grpcLis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		return err
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("notifications-bench gRPC server listening", "port", cfg.GRPCPort)
		if err := grpcSrv.Serve(grpcLis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Error("notifications-bench gRPC server error", "err", err)
			cancel()
		}
	}()

	healthSrv := &http.Server{
		Addr:         ":" + cfg.HTTPPort,
		ReadTimeout:  5 * time.Second,
		WriteTimeout: 5 * time.Second,
	}
	http.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("notifications-bench health server listening", "port", cfg.HTTPPort)
		if err := healthSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Error("health server error", "err", err)
			cancel()
		}
	}()

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	log.Info("shutdown signal received")
	cancel()

	grpcSrv.GracefulStop()

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := healthSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("health server shutdown", "err", err)
	}

	wg.Wait()
	log.Info("shutdown complete")
	return nil
}
