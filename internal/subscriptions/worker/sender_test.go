package worker

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/mail"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/subscriptions"
)

type fakeMailSender struct {
	mu       sync.Mutex
	maxBatch int
	err      error
	batches  [][]mail.Message
	ctxs     []context.Context
	ctxErrs  []error
	calls    chan []mail.Message
}

func newFakeMailSender(maxBatch int) *fakeMailSender {
	return &fakeMailSender{
		maxBatch: maxBatch,
		calls:    make(chan []mail.Message, 16),
	}
}

func (f *fakeMailSender) SendBatch(ctx context.Context, messages []mail.Message) error {
	batch := append([]mail.Message(nil), messages...)

	f.mu.Lock()
	f.batches = append(f.batches, batch)
	f.ctxs = append(f.ctxs, ctx)
	f.ctxErrs = append(f.ctxErrs, ctx.Err())
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

func waitForSenderDone(t *testing.T, done <-chan struct{}) {
	t.Helper()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for sender to stop")
	}
}

func assertNoBatch(t *testing.T, sender *fakeMailSender) {
	t.Helper()

	select {
	case batch := <-sender.calls:
		t.Fatalf("unexpected email batch: %+v", batch)
	default:
	}
}

func assertMailBatches(t *testing.T, sender *fakeMailSender, want [][]mail.Message) {
	t.Helper()

	sender.mu.Lock()
	got := append([][]mail.Message(nil), sender.batches...)
	sender.mu.Unlock()

	if !reflect.DeepEqual(got, want) {
		t.Fatalf("batches = %+v, want %+v", got, want)
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
		StartSender(ctx, sender, ch, maxWait, nil)
	}()
	return done
}

type recordingSenderLogger struct {
	warnings []recordedSenderLog
	errors   []recordedSenderLog
}

type recordedSenderLog struct {
	msg  string
	args []any
}

func (l *recordingSenderLogger) Debug(string, ...any) {}

func (l *recordingSenderLogger) Info(string, ...any) {}

func (l *recordingSenderLogger) Warn(msg string, args ...any) {
	l.warnings = append(l.warnings, recordedSenderLog{msg: msg, args: append([]any(nil), args...)})
}

func (l *recordingSenderLogger) Error(msg string, args ...any) {
	l.errors = append(l.errors, recordedSenderLog{msg: msg, args: append([]any(nil), args...)})
}

func (l *recordingSenderLogger) ErrorContext(context.Context, string, ...any) {}

type senderContextKey struct{}

func TestStartSenderFlushesAtProviderBatchSize(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sender := newFakeMailSender(2)
	ch := make(chan mail.Message, 2)
	done := runSender(ctx, sender, ch, time.Hour)

	first := mail.Message{To: "one@example.com", Subject: "one", HTML: "<p>one</p>"}
	second := mail.Message{To: "two@example.com", Subject: "two", HTML: "<p>two</p>"}
	ch <- first
	ch <- second

	batch := waitForBatch(t, sender)
	if !reflect.DeepEqual(batch, []mail.Message{first, second}) {
		t.Fatalf("batch = %+v, want first and second messages in order", batch)
	}

	close(ch)
	waitForSenderDone(t, done)
	assertMailBatches(t, sender, [][]mail.Message{{first, second}})
}

func TestStartSenderFlushesOnTicker(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sender := newFakeMailSender(10)
	ch := make(chan mail.Message, 1)
	done := runSender(ctx, sender, ch, 10*time.Millisecond)

	msg := mail.Message{To: "one@example.com", Subject: "subject", HTML: "<p>body</p>"}
	ch <- msg

	batch := waitForBatch(t, sender)
	if !reflect.DeepEqual(batch, []mail.Message{msg}) {
		t.Fatalf("batch = %+v, want queued message", batch)
	}

	close(ch)
	waitForSenderDone(t, done)
}

func TestStartSenderFlushesRemainingBufferWhenChannelClosed(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	sender := newFakeMailSender(10)
	ch := make(chan mail.Message, 1)
	done := runSender(ctx, sender, ch, time.Hour)

	msg := mail.Message{To: "one@example.com", Subject: "subject"}
	ch <- msg
	close(ch)

	batch := waitForBatch(t, sender)
	if !reflect.DeepEqual(batch, []mail.Message{msg}) {
		t.Fatalf("batch = %+v, want queued message", batch)
	}
	waitForSenderDone(t, done)
}

func TestStartSenderDrainsOnContextCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	sender := newFakeMailSender(2)
	ch := make(chan mail.Message, 3)
	first := mail.Message{To: "one@example.com"}
	second := mail.Message{To: "two@example.com"}
	third := mail.Message{To: "three@example.com"}
	ch <- first
	ch <- second
	ch <- third

	cancel()
	done := runSender(ctx, sender, ch, time.Hour)

	firstBatch := waitForBatch(t, sender)
	secondBatch := waitForBatch(t, sender)
	if !reflect.DeepEqual(firstBatch, []mail.Message{{To: "one@example.com"}, {To: "two@example.com"}}) {
		t.Fatalf("first drained batch = %+v", firstBatch)
	}
	if !reflect.DeepEqual(secondBatch, []mail.Message{{To: "three@example.com"}}) {
		t.Fatalf("second drained batch = %+v", secondBatch)
	}

	waitForSenderDone(t, done)
}

func TestStartSenderUsesFreshContextForFinalFlushAfterCancel(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), senderContextKey{}, "request-123"))
	sender := newFakeMailSender(10)
	ch := make(chan mail.Message, 1)
	ch <- mail.Message{To: "one@example.com"}

	cancel()
	done := runSender(ctx, sender, ch, time.Hour)

	waitForBatch(t, sender)
	waitForSenderDone(t, done)

	sender.mu.Lock()
	defer sender.mu.Unlock()
	if len(sender.ctxs) != 1 {
		t.Fatalf("send contexts = %d, want 1", len(sender.ctxs))
	}
	if err := sender.ctxErrs[0]; err != nil {
		t.Fatalf("final flush context err = %v, want nil", err)
	}
	if got := sender.ctxs[0].Value(senderContextKey{}); got != "request-123" {
		t.Fatalf("final flush context value = %v, want request-123", got)
	}
}

func TestFlushBufferDropsProviderErrors(t *testing.T) {
	t.Parallel()

	sender := newFakeMailSender(2)
	sender.err = errors.New("provider unavailable")

	buf := []mail.Message{
		{To: "one@example.com"},
		{To: "two@example.com"},
	}

	got := flushBuffer(context.Background(), sender, buf, 2, nil)
	if len(got) != 0 {
		t.Fatalf("remaining buffer size = %d, want 0", len(got))
	}

	batch := waitForBatch(t, sender)
	if len(batch) != 2 {
		t.Fatalf("batch size = %d, want 2", len(batch))
	}
}

func TestFlushBufferSplitsIntoProviderSizedChunks(t *testing.T) {
	t.Parallel()

	sender := newFakeMailSender(2)
	messages := []mail.Message{
		{To: "one@example.com"},
		{To: "two@example.com"},
		{To: "three@example.com"},
		{To: "four@example.com"},
		{To: "five@example.com"},
	}

	got := flushBuffer(context.Background(), sender, messages, 2, nil)
	if len(got) != 0 {
		t.Fatalf("remaining buffer size = %d, want 0", len(got))
	}

	assertMailBatches(t, sender, [][]mail.Message{
		messages[0:2],
		messages[2:4],
		messages[4:5],
	})
}

func TestFlushBufferEmptyDoesNotSend(t *testing.T) {
	t.Parallel()

	sender := newFakeMailSender(2)
	buf := make([]mail.Message, 0, 2)

	got := flushBuffer(context.Background(), sender, buf, 2, nil)
	if len(got) != 0 || cap(got) != 2 {
		t.Fatalf("buffer = len %d cap %d, want original empty buffer", len(got), cap(got))
	}
	assertNoBatch(t, sender)
}

func TestSenderBatchSizeFallsBackToOne(t *testing.T) {
	t.Parallel()

	sender := newFakeMailSender(0)

	if got := senderBatchSize(sender); got != 1 {
		t.Fatalf("batch size = %d, want 1", got)
	}
}

func TestChunkMessages(t *testing.T) {
	t.Parallel()

	messages := []mail.Message{
		{To: "one@example.com"},
		{To: "two@example.com"},
		{To: "three@example.com"},
	}

	tests := []struct {
		name      string
		batchSize int
		want      [][]mail.Message
	}{
		{"larger than messages", 10, [][]mail.Message{messages}},
		{"exact chunks", 1, [][]mail.Message{messages[0:1], messages[1:2], messages[2:3]}},
		{"falls back to one", 0, [][]mail.Message{messages[0:1], messages[1:2], messages[2:3]}},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := chunkMessages(messages, tc.batchSize)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("chunks = %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestDrainEmailChannelStopsWhenChannelEmptyWithoutBlocking(t *testing.T) {
	t.Parallel()

	sender := newFakeMailSender(2)
	ch := make(chan mail.Message, 3)
	buf := []mail.Message{{To: "buffered@example.com"}}
	ch <- mail.Message{To: "one@example.com"}
	ch <- mail.Message{To: "two@example.com"}

	got := drainEmailChannel(context.Background(), sender, ch, buf, 2, nil)

	if !reflect.DeepEqual(got, []mail.Message{{To: "two@example.com"}}) {
		t.Fatalf("remaining buffer = %+v, want last unflushed message", got)
	}
	assertMailBatches(t, sender, [][]mail.Message{{
		{To: "buffered@example.com"},
		{To: "one@example.com"},
	}})
}

func TestLogSendError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantWarns  int
		wantErrors int
		wantMsg    string
	}{
		{
			name:       "generic error",
			err:        errors.New("provider unavailable"),
			wantErrors: 1,
			wantMsg:    "sender: send batch failed",
		},
		{
			name:      "rate limit",
			err:       &subscriptions.RateLimitError{Service: "email", RetryAfter: time.Minute},
			wantWarns: 1,
			wantMsg:   "sender: email provider rate limited, dropping batch",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			log := &recordingSenderLogger{}

			logSendError(log, tc.err, 3)

			if len(log.warnings) != tc.wantWarns {
				t.Fatalf("warnings = %d, want %d", len(log.warnings), tc.wantWarns)
			}
			if len(log.errors) != tc.wantErrors {
				t.Fatalf("errors = %d, want %d", len(log.errors), tc.wantErrors)
			}

			var record recordedSenderLog
			if tc.wantWarns == 1 {
				record = log.warnings[0]
			} else {
				record = log.errors[0]
			}
			if record.msg != tc.wantMsg {
				t.Fatalf("message = %q, want %q", record.msg, tc.wantMsg)
			}
			args := fmt.Sprint(record.args)
			if !strings.Contains(args, "batch_size") || !strings.Contains(args, "3") {
				t.Fatalf("args missing batch size: %#v", record.args)
			}
			if strings.Contains(fmt.Sprint(record.msg, record.args), "user@example.com") {
				t.Fatalf("log contains email PII: %q", fmt.Sprint(record.msg, record.args))
			}
		})
	}
}
