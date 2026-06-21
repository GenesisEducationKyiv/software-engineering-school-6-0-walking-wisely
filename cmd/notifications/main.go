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

	"google.golang.org/grpc"
	"google.golang.org/grpc/reflection"

	notificationv1 "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/gen/notification/v1"
	contractcommands "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/commands"
	contractevents "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/events"
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

	// Register event types so the JetStream consumer can decode them.
	_ "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/release_monitoring/domain"
	_ "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/app"
)

func main() {
	log := platformlogger.NewStructured(os.Stdout, platformlogger.StructuredConfig{})
	if err := run(log); err != nil {
		log.Error("notifications service startup failed", "err", err)
		os.Exit(1)
	}
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
		ServiceName: cfg.ServiceName,
		Environment: cfg.Environment,
	})

	db, err := platformpostgres.NewDB(cfg.DatabaseURL, log)
	if err != nil {
		return err
	}
	defer db.Close()

	natsClient, err := platformnats.NewClient(cfg.NATSURL, cfg.ServiceName, log)
	if err != nil {
		return err
	}
	defer natsClient.Close()

	// Outbox for saga reply events (notifications_outbox => NATS).
	notificationsOutboxRepo := outbox.NewRepository(db, "notifications_outbox")
	notificationsOutboxPublisher, err := platformnats.NewPublisher(natsClient, platformnats.PublisherOptions{
		StreamName:    cfg.NATSStreamName,
		SubjectPrefix: cfg.NATSSubjectPrefix,
	})
	if err != nil {
		return err
	}

	notificationJobRepo := notificationpostgres.NewRepository(db, notificationsOutboxRepo, cfg.JobInsertBatchSize)
	resendClient := resend.NewClient(cfg.ResendAPIKey, cfg.FromEmail, log)

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
		notificationworker.StartSender(ctx, resendClient, notificationJobRepo, cfg.ResendMaxWait, log)
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		notificationworker.StartCleanup(ctx, notificationJobRepo, cfg.JobCleanupInterval, cfg.JobRetention, log)
	}()

	// Dispatcher for the notifications outbox — publishes saga reply events to NATS.
	wg.Add(1)
	go func() {
		defer wg.Done()
		outbox.StartDispatcher(ctx, notificationsOutboxRepo, notificationsOutboxPublisher, cfg.NotificationsOutboxInterval, 32, 5, log)
	}()

	// Cleanup for the notifications outbox.
	wg.Add(1)
	go func() {
		defer wg.Done()
		outbox.StartCleanup(ctx, notificationsOutboxRepo, cfg.NotificationsOutboxInterval, cfg.NotificationsOutboxRetention, log)
	}()

	// Internal gRPC server — accepts SendConfirmation calls from the subscriptions
	// service when SAGA_TRANSPORT=grpc is set on that side.
	grpcSrv := grpc.NewServer()
	notificationv1.RegisterNotificationServiceServer(grpcSrv, notificationgrpc.NewServer(notificationHandlers))
	reflection.Register(grpcSrv)

	grpcLis, err := net.Listen("tcp", ":"+cfg.GRPCPort)
	if err != nil {
		return err
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		log.Info("notifications gRPC server listening", "port", cfg.GRPCPort)
		if err := grpcSrv.Serve(grpcLis); err != nil && !errors.Is(err, grpc.ErrServerStopped) {
			log.Error("notifications gRPC server error", "err", err)
			cancel()
		}
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
