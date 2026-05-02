package workers

import (
	"context"
	"log/slog"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/clients"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/domain"
)

// StartSender reads email messages from emailChan and delivers them in batches
// via the Resend API. Batches are flushed every maxWait (default 200 ms) or
// when they reach clients.ResendBatchMax (100), whichever comes first.
//
// Backpressure is achieved by letting the upstream channel fill up: callers
// that send to a full channel either block or drop (scanner uses non-blocking
// send with a default case).
//
// On context cancellation the worker drains any remaining buffered messages
// and attempts a final flush before returning.
func StartSender(
	ctx context.Context,
	resend *clients.ResendClient,
	emailChan <-chan domain.EmailMessage,
	maxWait time.Duration,
) {
	slog.Info("sender started", "max_wait", maxWait)
	ticker := time.NewTicker(maxWait)
	defer ticker.Stop()

	buf := make([]domain.EmailMessage, 0, clients.ResendBatchMax)

	// flushWith sends everything currently in buf using the supplied context,
	// then resets buf. Rate-limit errors are logged and the batch is dropped
	// (notifications are best-effort; we do not re-queue to avoid storms).
	flushWith := func(flushCtx context.Context) {
		if len(buf) == 0 {
			return
		}
		toSend := buf
		buf = make([]domain.EmailMessage, 0, clients.ResendBatchMax)

		for i := 0; i < len(toSend); i += clients.ResendBatchMax {
			end := i + clients.ResendBatchMax
			if end > len(toSend) {
				end = len(toSend)
			}
			chunk := toSend[i:end]
			if err := resend.SendBatch(flushCtx, chunk); err != nil {
				if rle, ok := domain.AsRateLimitError(err); ok {
					slog.Warn("sender: resend rate limited, dropping batch",
						"batch_size", len(chunk), "retry_after", rle.RetryAfter)
				} else {
					slog.Error("sender: send batch failed",
						"batch_size", len(chunk), "err", err)
				}
			}
		}
	}

	for {
		select {
		case msg, ok := <-emailChan:
			if !ok {
				// Channel closed - flush and exit.
				flushWith(context.Background())
				slog.Info("sender stopped (channel closed)")
				return
			}
			buf = append(buf, msg)
			if len(buf) >= clients.ResendBatchMax {
				flushWith(ctx)
			}

		case <-ticker.C:
			flushWith(ctx)

		case <-ctx.Done():
			// Drain any messages already in the channel before shutting down.
			// Use a fresh background context so the final HTTP calls are not
			// cancelled immediately.
			drainCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		drain:
			for {
				select {
				case msg := <-emailChan:
					buf = append(buf, msg)
					if len(buf) >= clients.ResendBatchMax {
						flushWith(drainCtx)
					}
				default:
					break drain
				}
			}
			flushWith(drainCtx)
			cancel()
			slog.Info("sender stopped")
			return
		}
	}
}
