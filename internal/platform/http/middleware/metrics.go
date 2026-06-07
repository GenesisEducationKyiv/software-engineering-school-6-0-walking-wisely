package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type MetricsRecorder interface {
	RecordHTTPRequest(ctx context.Context, method, path string, status int, duration time.Duration)
	RegisterEmailChannelDepth(depth func() int) error
	RegisterOutboxMetrics(snapshot func(context.Context) (pendingCount int64, oldestPendingAge float64, retryCount int64, failedCount int64, err error)) error
}

type OpenTelemetryRecorder struct {
	meter               metric.Meter
	httpRequestsTotal   metric.Int64Counter
	httpRequestDuration metric.Float64Histogram
}

func NewOpenTelemetryRecorder(meter metric.Meter) (*OpenTelemetryRecorder, error) {
	httpRequestsTotal, err := meter.Int64Counter(
		"http_requests_total",
		metric.WithDescription("Total HTTP requests handled."),
		metric.WithUnit("{request}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create http requests counter: %w", err)
	}

	httpRequestDuration, err := meter.Float64Histogram(
		"http_request_duration_seconds",
		metric.WithDescription("HTTP request latency."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("create http request duration histogram: %w", err)
	}

	return &OpenTelemetryRecorder{
		meter:               meter,
		httpRequestsTotal:   httpRequestsTotal,
		httpRequestDuration: httpRequestDuration,
	}, nil
}

func (r *OpenTelemetryRecorder) RegisterEmailChannelDepth(depth func() int) error {
	gauge, err := r.meter.Int64ObservableGauge(
		"email_channel_depth",
		metric.WithDescription("Number of pending emails in the send queue."),
		metric.WithUnit("{email}"),
	)
	if err != nil {
		return fmt.Errorf("create email channel depth gauge: %w", err)
	}

	_, err = r.meter.RegisterCallback(func(_ context.Context, observer metric.Observer) error {
		observer.ObserveInt64(gauge, int64(depth()))
		return nil
	}, gauge)
	if err != nil {
		return fmt.Errorf("register email channel depth callback: %w", err)
	}

	return nil
}

func (r *OpenTelemetryRecorder) RegisterOutboxMetrics(
	snapshot func(context.Context) (pendingCount int64, oldestPendingAge float64, retryCount int64, failedCount int64, err error),
) error {
	pendingGauge, err := r.meter.Int64ObservableGauge(
		"outbox_pending_count",
		metric.WithDescription("Number of pending outbox events."),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		return fmt.Errorf("create outbox pending count gauge: %w", err)
	}

	oldestAgeGauge, err := r.meter.Float64ObservableGauge(
		"outbox_oldest_pending_age_seconds",
		metric.WithDescription("Age in seconds of the oldest pending outbox event."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return fmt.Errorf("create outbox oldest pending age gauge: %w", err)
	}

	retryGauge, err := r.meter.Int64ObservableGauge(
		"outbox_retry_count",
		metric.WithDescription("Number of outbox events currently scheduled for retry."),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		return fmt.Errorf("create outbox retry count gauge: %w", err)
	}

	failedGauge, err := r.meter.Int64ObservableGauge(
		"outbox_failed_count",
		metric.WithDescription("Number of outbox events in failed state."),
		metric.WithUnit("{event}"),
	)
	if err != nil {
		return fmt.Errorf("create outbox failed count gauge: %w", err)
	}

	_, err = r.meter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		pendingCount, oldestPendingAge, retryCount, failedCount, err := snapshot(ctx)
		if err != nil {
			return err
		}

		observer.ObserveInt64(pendingGauge, pendingCount)
		observer.ObserveFloat64(oldestAgeGauge, oldestPendingAge)
		observer.ObserveInt64(retryGauge, retryCount)
		observer.ObserveInt64(failedGauge, failedCount)
		return nil
	}, pendingGauge, oldestAgeGauge, retryGauge, failedGauge)
	if err != nil {
		return fmt.Errorf("register outbox metrics callback: %w", err)
	}

	return nil
}

func (r *OpenTelemetryRecorder) RecordHTTPRequest(ctx context.Context, method, path string, status int, duration time.Duration) {
	attrs := metric.WithAttributes(
		attribute.String("method", method),
		attribute.String("path", path),
		attribute.String("status", strconv.Itoa(status)),
	)

	r.httpRequestsTotal.Add(ctx, 1, attrs)
	r.httpRequestDuration.Record(
		ctx,
		duration.Seconds(),
		attrs,
	)
}

// Metrics wraps h and records HTTP request metrics.
func Metrics(h http.Handler, recorder MetricsRecorder) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		h.ServeHTTP(rec, r)

		recorder.RecordHTTPRequest(r.Context(), r.Method, r.URL.Path, rec.status, time.Since(start))
	})
}
