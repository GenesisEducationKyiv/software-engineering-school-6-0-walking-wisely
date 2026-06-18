package worker

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts/mail"
	notificationdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/domain"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
)

type JobQueue interface {
	ClaimPending(ctx context.Context, workerID string, batchSize int) ([]notificationdomain.Job, error)
	MarkSent(ctx context.Context, jobs []notificationdomain.Job) error
	MarkFailed(ctx context.Context, jobs []notificationdomain.Job, maxAttempts int, cause error) error
}

func StartSender(
	ctx context.Context,
	sender mail.Sender,
	jobs JobQueue,
	pollInterval time.Duration,
	log logger.Logger,
) {
	if log == nil {
		log = logger.NoopLogger{}
	}

	if pollInterval <= 0 {
		pollInterval = 200 * time.Millisecond
	}

	log.Info("sender started", "poll_interval", pollInterval)
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	batchSize := senderBatchSize(sender)
	workerID := uuid.NewString()
	const maxAttempts = 5

	for {
		select {
		case <-ticker.C:
			if err := flushPending(ctx, sender, jobs, workerID, batchSize, maxAttempts, log); err != nil {
				log.Error("sender: flush pending failed", "err", err)
			}
		case <-ctx.Done():
			log.Info("sender stopped")
			return
		}
	}
}

func senderBatchSize(sender mail.Sender) int {
	batchSize := sender.MaxBatchSize()
	if batchSize < 1 {
		return 1
	}
	return batchSize
}

func flushPending(
	flushCtx context.Context,
	sender mail.Sender,
	jobs JobQueue,
	workerID string,
	batchSize int,
	maxAttempts int,
	log logger.Logger,
) error {
	if log == nil {
		log = logger.NoopLogger{}
	}
	claimed, err := jobs.ClaimPending(flushCtx, workerID, batchSize)
	if err != nil {
		return err
	}
	if len(claimed) == 0 {
		return nil
	}

	messages := make([]mail.Message, 0, len(claimed))
	for i := range claimed {
		messages = append(messages, mail.Message{
			To:      claimed[i].To,
			Subject: claimed[i].Subject,
			HTML:    claimed[i].HTML,
		})
	}

	if err := sender.SendBatch(flushCtx, messages); err != nil {
		logSendError(log, err, len(messages))
		if markErr := jobs.MarkFailed(flushCtx, claimed, maxAttempts, err); markErr != nil {
			return markErr
		}
		return nil
	}

	if err := jobs.MarkSent(flushCtx, claimed); err != nil {
		return err
	}
	log.Info("sender: batch sent", "batch_size", len(messages))
	return nil
}

func logSendError(log logger.Logger, err error, batchSize int) {
	if log == nil {
		log = logger.NoopLogger{}
	}
	var rle *contracts.RateLimitError
	if ok := errors.As(err, &rle); ok {
		log.Warn("sender: email provider rate limited, dropping batch",
			"batch_size", batchSize, "retry_after", rle.RetryAfter)
		return
	}
	log.Error("sender: send batch failed", "batch_size", batchSize, "err", err)
}
