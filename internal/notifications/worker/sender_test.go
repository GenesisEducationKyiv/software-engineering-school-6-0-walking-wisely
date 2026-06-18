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

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/contracts"
	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/mail"
	notificationpostgres "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/notifications/postgres"
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

type fakeJobQueue struct {
	mu              sync.Mutex
	claimErr        error
	markSentErr     error
	markFailedErr   error
	claims          [][]notificationpostgres.Job
	claimCalls      int
	markSentCalls   [][]notificationpostgres.Job
	markFailedCalls []failedMarkCall
	workerIDs       []string
	ctxs            []context.Context
}

type failedMarkCall struct {
	jobs        []notificationpostgres.Job
	maxAttempts int
	cause       error
}

func (f *fakeJobQueue) ClaimPending(ctx context.Context, workerID string, batchSize int) ([]notificationpostgres.Job, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.claimCalls++
	f.workerIDs = append(f.workerIDs, workerID)
	f.ctxs = append(f.ctxs, ctx)
	if f.claimErr != nil {
		return nil, f.claimErr
	}
	if len(f.claims) == 0 {
		return nil, nil
	}

	jobs := append([]notificationpostgres.Job(nil), f.claims[0]...)
	f.claims = f.claims[1:]
	if len(jobs) > batchSize {
		jobs = jobs[:batchSize]
	}
	return jobs, nil
}

func (f *fakeJobQueue) MarkSent(_ context.Context, jobs []notificationpostgres.Job) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markSentCalls = append(f.markSentCalls, append([]notificationpostgres.Job(nil), jobs...))
	return f.markSentErr
}

func (f *fakeJobQueue) MarkFailed(_ context.Context, jobs []notificationpostgres.Job, maxAttempts int, cause error) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.markFailedCalls = append(f.markFailedCalls, failedMarkCall{
		jobs:        append([]notificationpostgres.Job(nil), jobs...),
		maxAttempts: maxAttempts,
		cause:       cause,
	})
	return f.markFailedErr
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

func runSender(
	ctx context.Context,
	sender mail.Sender,
	jobs JobQueue,
	pollInterval time.Duration,
	log *recordingSenderLogger,
) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		defer close(done)
		StartSender(ctx, sender, jobs, pollInterval, log)
	}()
	return done
}

type recordingSenderLogger struct {
	warnings []recordedSenderLog
	errors   []recordedSenderLog
	infos    []recordedSenderLog
}

type recordedSenderLog struct {
	msg  string
	args []any
}

func (l *recordingSenderLogger) Debug(string, ...any) {}
func (l *recordingSenderLogger) Info(msg string, args ...any) {
	l.infos = append(l.infos, recordedSenderLog{msg: msg, args: append([]any(nil), args...)})
}

func (l *recordingSenderLogger) Warn(msg string, args ...any) {
	l.warnings = append(l.warnings, recordedSenderLog{msg: msg, args: append([]any(nil), args...)})
}

func (l *recordingSenderLogger) Error(msg string, args ...any) {
	l.errors = append(l.errors, recordedSenderLog{msg: msg, args: append([]any(nil), args...)})
}
func (l *recordingSenderLogger) ErrorContext(context.Context, string, ...any) {}

type senderContextKey struct{}

func TestStartSenderFlushesClaimedJobsAtProviderBatchSize(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sender := newFakeMailSender(2)
	jobs := &fakeJobQueue{
		claims: [][]notificationpostgres.Job{{
			{ID: "job-1", To: "one@example.com", Subject: "one", HTML: "<p>one</p>"},
			{ID: "job-2", To: "two@example.com", Subject: "two", HTML: "<p>two</p>"},
		}},
	}
	done := runSender(ctx, sender, jobs, time.Millisecond, &recordingSenderLogger{})

	batch := waitForBatch(t, sender)
	if !reflect.DeepEqual(batch, []mail.Message{
		{To: "one@example.com", Subject: "one", HTML: "<p>one</p>"},
		{To: "two@example.com", Subject: "two", HTML: "<p>two</p>"},
	}) {
		t.Fatalf("batch = %+v, want queued messages in order", batch)
	}

	cancel()
	waitForSenderDone(t, done)
	if len(jobs.markSentCalls) != 1 {
		t.Fatalf("mark sent calls = %d, want 1", len(jobs.markSentCalls))
	}
}

func TestStartSenderDoesNothingWhenNoJobsClaimed(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	sender := newFakeMailSender(10)
	jobs := &fakeJobQueue{}
	done := runSender(ctx, sender, jobs, time.Millisecond, &recordingSenderLogger{})

	time.Sleep(20 * time.Millisecond)
	cancel()
	waitForSenderDone(t, done)

	assertNoBatch(t, sender)
	if len(jobs.markSentCalls) != 0 || len(jobs.markFailedCalls) != 0 {
		t.Fatalf("unexpected marks: sent=%d failed=%d", len(jobs.markSentCalls), len(jobs.markFailedCalls))
	}
}

func TestStartSenderPassesContextToDependencies(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), senderContextKey{}, "request-123"))
	defer cancel()

	sender := newFakeMailSender(1)
	jobs := &fakeJobQueue{
		claims: [][]notificationpostgres.Job{{{
			ID: "job-1", To: "one@example.com", Subject: "one", HTML: "<p>one</p>",
		}}},
	}
	done := runSender(ctx, sender, jobs, time.Millisecond, &recordingSenderLogger{})

	waitForBatch(t, sender)
	cancel()
	waitForSenderDone(t, done)

	sender.mu.Lock()
	if len(sender.ctxs) != 1 {
		sender.mu.Unlock()
		t.Fatalf("send contexts = %d, want 1", len(sender.ctxs))
	}
	sendCtx := sender.ctxs[0]
	sender.mu.Unlock()

	if got := sendCtx.Value(senderContextKey{}); got != "request-123" {
		t.Fatalf("send context value = %v, want request-123", got)
	}
	jobs.mu.Lock()
	defer jobs.mu.Unlock()
	if len(jobs.ctxs) == 0 || jobs.ctxs[0] != ctx {
		t.Fatalf("claim contexts = %#v, want original context", jobs.ctxs)
	}
}

func TestFlushPendingProviderErrorMarksJobsFailed(t *testing.T) {
	t.Parallel()

	sender := newFakeMailSender(2)
	sender.err = errors.New("provider unavailable")
	jobs := &fakeJobQueue{
		claims: [][]notificationpostgres.Job{{
			{ID: "job-1", To: "one@example.com", Subject: "one"},
			{ID: "job-2", To: "two@example.com", Subject: "two"},
		}},
	}

	err := flushPending(context.Background(), sender, jobs, "worker-1", 2, 5, &recordingSenderLogger{})
	if err != nil {
		t.Fatalf("flushPending returned error: %v", err)
	}

	if len(jobs.markFailedCalls) != 1 {
		t.Fatalf("mark failed calls = %d, want 1", len(jobs.markFailedCalls))
	}
	call := jobs.markFailedCalls[0]
	if call.maxAttempts != 5 {
		t.Fatalf("maxAttempts = %d, want 5", call.maxAttempts)
	}
	if !errors.Is(call.cause, sender.err) {
		t.Fatalf("cause = %v, want provider error", call.cause)
	}
	if len(jobs.markSentCalls) != 0 {
		t.Fatalf("mark sent calls = %d, want 0", len(jobs.markSentCalls))
	}
}

func TestFlushPendingSuccessfulSendMarksJobsSent(t *testing.T) {
	t.Parallel()

	sender := newFakeMailSender(2)
	jobs := &fakeJobQueue{
		claims: [][]notificationpostgres.Job{{
			{ID: "job-1", To: "one@example.com", Subject: "one", HTML: "<p>one</p>"},
			{ID: "job-2", To: "two@example.com", Subject: "two", HTML: "<p>two</p>"},
		}},
	}

	err := flushPending(context.Background(), sender, jobs, "worker-1", 2, 5, &recordingSenderLogger{})
	if err != nil {
		t.Fatalf("flushPending returned error: %v", err)
	}

	if len(jobs.markSentCalls) != 1 {
		t.Fatalf("mark sent calls = %d, want 1", len(jobs.markSentCalls))
	}
	if len(jobs.markFailedCalls) != 0 {
		t.Fatalf("mark failed calls = %d, want 0", len(jobs.markFailedCalls))
	}
}

func TestFlushPendingClaimErrorIsReturned(t *testing.T) {
	t.Parallel()

	sender := newFakeMailSender(2)
	jobs := &fakeJobQueue{claimErr: errors.New("db unavailable")}

	err := flushPending(context.Background(), sender, jobs, "worker-1", 2, 5, &recordingSenderLogger{})
	if !errors.Is(err, jobs.claimErr) {
		t.Fatalf("error = %v, want %v", err, jobs.claimErr)
	}
	assertNoBatch(t, sender)
}

func TestFlushPendingMarkSentErrorIsReturned(t *testing.T) {
	t.Parallel()

	sender := newFakeMailSender(2)
	jobs := &fakeJobQueue{
		markSentErr: errors.New("db unavailable"),
		claims: [][]notificationpostgres.Job{{
			{ID: "job-1", To: "one@example.com", Subject: "one"},
		}},
	}

	err := flushPending(context.Background(), sender, jobs, "worker-1", 2, 5, &recordingSenderLogger{})
	if !errors.Is(err, jobs.markSentErr) {
		t.Fatalf("error = %v, want %v", err, jobs.markSentErr)
	}
}

func TestFlushPendingMarkFailedErrorIsReturned(t *testing.T) {
	t.Parallel()

	sender := newFakeMailSender(2)
	sender.err = errors.New("provider unavailable")
	jobs := &fakeJobQueue{
		markFailedErr: errors.New("db unavailable"),
		claims: [][]notificationpostgres.Job{{
			{ID: "job-1", To: "one@example.com", Subject: "one"},
		}},
	}

	err := flushPending(context.Background(), sender, jobs, "worker-1", 2, 5, &recordingSenderLogger{})
	if !errors.Is(err, jobs.markFailedErr) {
		t.Fatalf("error = %v, want %v", err, jobs.markFailedErr)
	}
}

func TestSenderBatchSizeFallsBackToOne(t *testing.T) {
	t.Parallel()

	sender := newFakeMailSender(0)

	if got := senderBatchSize(sender); got != 1 {
		t.Fatalf("batch size = %d, want 1", got)
	}
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
			err:       &contracts.RateLimitError{Service: "email", RetryAfter: time.Minute},
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
