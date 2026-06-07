package outbox

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

func StartDispatcher(
	ctx context.Context,
	repo *Repository,
	bus events.Publisher,
	pollInterval time.Duration,
	batchSize int,
	maxAttempts int,
	log logger.Logger,
) {
	if log == nil {
		log = logger.NoopLogger{}
	}
	if pollInterval <= 0 {
		pollInterval = 200 * time.Millisecond
	}
	if batchSize < 1 {
		batchSize = 1
	}
	if maxAttempts < 1 {
		maxAttempts = 5
	}

	workerID := uuid.NewString()
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	log.Info("outbox dispatcher started", "poll_interval", pollInterval, "batch_size", batchSize)

	for {
		select {
		case <-ctx.Done():
			log.Info("outbox dispatcher stopped")
			return
		case <-ticker.C:
			if err := dispatchBatch(ctx, repo, bus, workerID, batchSize, maxAttempts, log); err != nil {
				log.Error("outbox dispatcher batch failed", "err", err)
			}
		}
	}
}

func dispatchBatch(
	ctx context.Context,
	repo *Repository,
	bus events.Publisher,
	workerID string,
	batchSize int,
	maxAttempts int,
	log logger.Logger,
) error {
	records, err := repo.ClaimPending(ctx, workerID, batchSize)
	if err != nil {
		return err
	}

	for _, record := range records {
		event, err := events.Decode(record.EventType, record.PayloadJSON)
		if err != nil {
			_ = repo.MarkFailed(ctx, record.ID, record.AttemptCount+1, maxAttempts, err)
			log.Error("outbox decode failed", "event_type", record.EventType, "event_id", record.ID, "err", err)
			continue
		}

		if err := bus.Publish(ctx, event); err != nil {
			attempts := record.AttemptCount + 1
			if markErr := repo.MarkFailed(ctx, record.ID, attempts, maxAttempts, err); markErr != nil {
				log.Error("outbox mark failed failed", "event_id", record.ID, "err", markErr)
			}
			log.Error("outbox handler failed", "event_type", record.EventType, "event_id", record.ID, "attempt_count", attempts, "err", err)
			continue
		}

		if err := repo.MarkDelivered(ctx, record.ID); err != nil {
			return fmt.Errorf("mark delivered %s: %w", record.ID, err)
		}
	}

	return nil
}
