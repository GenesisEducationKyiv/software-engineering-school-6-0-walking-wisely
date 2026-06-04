package worker

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
)

type fakeMailSender struct {
	mu       sync.Mutex
	maxBatch int
	err      error
	batches  [][]mail.Message
	calls    chan []mail.Message
}

func newFakeMailSender(maxBatch int) *fakeMailSender {
	return &fakeMailSender{
		maxBatch: maxBatch,
		calls:    make(chan []mail.Message, 16),
	}
}

func (f *fakeMailSender) SendBatch(_ context.Context, messages []mail.Message) error {
	batch := append([]mail.Message(nil), messages...)

	f.mu.Lock()
	f.batches = append(f.batches, batch)
	f.mu.Unlock()

	f.calls <- batch
	return f.err
}

func (f *fakeMailSender) MaxBatchSize() int {
	return f.maxBatch
}

func waitForBatch(t *testing.T, sender *fakeMailSender) []mail.Message {
	t.Helper()

	select {
	case batch := <-sender.calls:
		return batch
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for email batch")
		return nil
	}
}

func runSender(
	ctx context.Context,
	sender mail.Sender,
	ch <-chan mail.Message,
	maxWait time.Duration,
) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		StartSender(ctx, sender, ch, maxWait)
	}()
	return done
}

func TestStartSenderFlushesAtProviderBatchSize(t *testing.T) {
	ctx := context.Background()
	sender := newFakeMailSender(2)
	ch := make(chan mail.Message, 2)
	done := runSender(ctx, sender, ch, time.Hour)

	ch <- mail.Message{To: "one@example.com"}
	ch <- mail.Message{To: "two@example.com"}

	batch := waitForBatch(t, sender)
	if len(batch) != 2 {
		t.Fatalf("batch size = %d, want 2", len(batch))
	}

	close(ch)
	<-done
}

func TestStartSenderFlushesOnTicker(t *testing.T) {
	ctx := context.Background()
	sender := newFakeMailSender(10)
	ch := make(chan mail.Message, 1)
	done := runSender(ctx, sender, ch, 10*time.Millisecond)

	ch <- mail.Message{To: "one@example.com"}

	batch := waitForBatch(t, sender)
	if len(batch) != 1 {
		t.Fatalf("batch size = %d, want 1", len(batch))
	}

	close(ch)
	<-done
}

func TestStartSenderDrainsOnContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	sender := newFakeMailSender(2)
	ch := make(chan mail.Message, 3)
	ch <- mail.Message{To: "one@example.com"}
	ch <- mail.Message{To: "two@example.com"}
	ch <- mail.Message{To: "three@example.com"}

	cancel()
	done := runSender(ctx, sender, ch, time.Hour)

	first := waitForBatch(t, sender)
	second := waitForBatch(t, sender)
	if len(first) != 2 || len(second) != 1 {
		t.Fatalf("drained batch sizes = [%d %d], want [2 1]", len(first), len(second))
	}

	<-done
}

func TestFlushBufferDropsProviderErrors(t *testing.T) {
	sender := newFakeMailSender(2)
	sender.err = errors.New("provider unavailable")

	buf := []mail.Message{
		{To: "one@example.com"},
		{To: "two@example.com"},
	}

	got := flushBuffer(context.Background(), sender, buf, 2)
	if len(got) != 0 {
		t.Fatalf("remaining buffer size = %d, want 0", len(got))
	}

	batch := waitForBatch(t, sender)
	if len(batch) != 2 {
		t.Fatalf("batch size = %d, want 2", len(batch))
	}
}

func TestSenderBatchSizeFallsBackToOne(t *testing.T) {
	sender := newFakeMailSender(0)

	if got := senderBatchSize(sender); got != 1 {
		t.Fatalf("batch size = %d, want 1", got)
	}
}
