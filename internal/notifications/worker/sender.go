package worker

import (
	"context"
	"errors"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	subscriptionsdomain "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions/domain"
)

func StartSender(
	ctx context.Context,
	sender mail.Sender,
	emailChan <-chan mail.Message,
	maxWait time.Duration,
	log logger.Logger,
) {
	if log == nil {
		log = logger.NoopLogger{}
	}

	log.Info("sender started", "max_wait", maxWait)
	ticker := time.NewTicker(maxWait)
	defer ticker.Stop()

	batchSize := senderBatchSize(sender)
	buf := make([]mail.Message, 0, batchSize)

	for {
		select {
		case msg, ok := <-emailChan:
			if !ok {
				flushBuffer(ctx, sender, buf, batchSize, log)
				log.Info("sender stopped (channel closed)")
				return
			}
			buf = append(buf, msg)
			if len(buf) >= batchSize {
				buf = flushBuffer(ctx, sender, buf, batchSize, log)
			}
		case <-ticker.C:
			buf = flushBuffer(ctx, sender, buf, batchSize, log)
		case <-ctx.Done():
			drainCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			buf = drainEmailChannel(drainCtx, sender, emailChan, buf, batchSize, log)
			flushBuffer(drainCtx, sender, buf, batchSize, log)
			cancel()
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

func flushBuffer(
	flushCtx context.Context,
	sender mail.Sender,
	buf []mail.Message,
	batchSize int,
	log logger.Logger,
) []mail.Message {
	if log == nil {
		log = logger.NoopLogger{}
	}
	if len(buf) == 0 {
		return buf
	}
	for _, chunk := range chunkMessages(buf, batchSize) {
		if err := sender.SendBatch(flushCtx, chunk); err != nil {
			logSendError(log, err, len(chunk))
		} else {
			log.Info("sender: batch sent", "batch_size", len(chunk))
		}
	}
	return make([]mail.Message, 0, batchSize)
}

func drainEmailChannel(
	ctx context.Context,
	sender mail.Sender,
	emailChan <-chan mail.Message,
	buf []mail.Message,
	batchSize int,
	log logger.Logger,
) []mail.Message {
	for {
		select {
		case msg, ok := <-emailChan:
			if !ok {
				return buf
			}
			buf = append(buf, msg)
			if len(buf) >= batchSize {
				buf = flushBuffer(ctx, sender, buf, batchSize, log)
			}
		default:
			return buf
		}
	}
}

func chunkMessages(messages []mail.Message, batchSize int) [][]mail.Message {
	if batchSize < 1 {
		batchSize = 1
	}
	chunks := make([][]mail.Message, 0, (len(messages)+batchSize-1)/batchSize)
	for i := 0; i < len(messages); i += batchSize {
		end := i + batchSize
		if end > len(messages) {
			end = len(messages)
		}
		chunks = append(chunks, messages[i:end])
	}
	return chunks
}

func logSendError(log logger.Logger, err error, batchSize int) {
	if log == nil {
		log = logger.NoopLogger{}
	}
	var rle *subscriptionsdomain.RateLimitError
	if ok := errors.As(err, &rle); ok {
		log.Warn("sender: email provider rate limited, dropping batch",
			"batch_size", batchSize, "retry_after", rle.RetryAfter)
		return
	}
	log.Error("sender: send batch failed", "batch_size", batchSize, "err", err)
}
