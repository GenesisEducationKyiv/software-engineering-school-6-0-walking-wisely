package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/commands"
	contractevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/integrations/resend"
	notificationapp "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/app"
	notificationpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/postgres"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/worker"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/config"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/nats"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/outbox"
	platformpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/postgres/migrations"
)

func main() {
	log := logger.NewStructured(os.Stdout, logger.StructuredConfig{})
	if err := run(log); err != nil {
		log.Error("notifications service startup failed", "err", err)
		os.Exit(1)
	}
}

func run(log logger.Logger) error {
	events.RegisterTypes(
		contractevents.SubscriptionRequested{},
		contractevents.ReleaseDetected{},
		commands.SendConfirmationEmail{},
		commands.ConfirmationEmailSent{},
		commands.ConfirmationEmailFailed{},
	)

	cfg, err := config.LoadNotificationsConfig()
	if err != nil {
		return err
	}

	log = logger.NewStructured(os.Stdout, logger.StructuredConfig{
		Level:       cfg.LogLevel,
		ServiceName: cfg.ServiceName,
		Environment: cfg.Environment,
	})

	db, err := platformpostgres.NewDB(cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer db.Close()

	if err := migrations.Run(cfg.DatabaseURL, log); err != nil {
		return fmt.Errorf("run database migrations: %w", err)
	}

	natsClient, err := nats.NewClient(cfg.NATS.URL, cfg.ServiceName, log)
	if err != nil {
		return err
	}
	defer natsClient.Close()

	// Outbox for saga reply events (notifications_outbox => NATS).
	notificationsOutboxRepo := outbox.NewRepository(db, "notifications_outbox")
	notificationsOutboxPublisher, err := nats.NewPublisher(natsClient, nats.PublisherOptions{
		StreamName:    cfg.NATS.StreamName,
		SubjectPrefix: cfg.NATS.SubjectPrefix,
	})
	if err != nil {
		return err
	}

	notificationJobRepo := notificationpostgres.NewRepository(db, notificationsOutboxRepo, cfg.Job.InsertBatchSize)
	resendClient := resend.NewClient(cfg.Resend.APIKey, cfg.Resend.From, log)

	bus := events.NewBus()
	notificationHandlers := notificationapp.NewEventHandlers(notificationJobRepo, cfg.Resend.BaseURL, log)
	notificationHandlers.Register(bus)

	consumer, err := nats.NewConsumer(
		natsClient, log,
		nats.WithStreamName(cfg.NATS.StreamName),
		nats.WithSubjectPrefix(cfg.NATS.SubjectPrefix),
		nats.WithConsumerName(cfg.NATS.ConsumerName),
		nats.WithBatchSize(cfg.NATS.BatchSize),
		nats.WithAckWait(cfg.NATS.AckWait),
		nats.WithMaxDeliveries(cfg.NATS.MaxDeliveries),
		nats.WithDLQSubject(cfg.NATS.DLQSubject),
	)
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
		worker.StartSender(ctx, resendClient, notificationJobRepo, cfg.Resend.MaxWait, log)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		worker.StartCleanup(ctx, notificationJobRepo, cfg.Job.CleanupInterval, cfg.Job.Retention, log)
	}()

	// Dispatcher for the notifications outbox — publishes saga reply events to NATS.
	wg.Add(1)
	go func() {
		defer wg.Done()
		outbox.StartDispatcher(ctx, notificationsOutboxRepo, notificationsOutboxPublisher, cfg.Outbox.CleanupInterval, 32, 5, log)
	}()

	// Cleanup for the notifications outbox.
	wg.Add(1)
	go func() {
		defer wg.Done()
		outbox.StartCleanup(ctx, notificationsOutboxRepo, cfg.Outbox.CleanupInterval, cfg.Outbox.Retention, log)
	}()

	// Minimal health endpoint for container orchestration readiness probes.
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
		log.Info("notifications health server listening", "port", cfg.HTTPPort)
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

	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer shutdownCancel()
	if err := healthSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("health server shutdown", "err", err)
	}

	wg.Wait()
	log.Info("shutdown complete")
	return nil
}
