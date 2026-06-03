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
}

type OpenTelemetryRecorder struct {
	meter               metric.Meter
	httpRequestDuration metric.Float64Histogram
}

func NewOpenTelemetryRecorder(meter metric.Meter) (*OpenTelemetryRecorder, error) {
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

func (r *OpenTelemetryRecorder) RecordHTTPRequest(ctx context.Context, method, path string, status int, duration time.Duration) {
	r.httpRequestDuration.Record(
		ctx,
		duration.Seconds(),
		metric.WithAttributes(
			attribute.String("method", method),
			attribute.String("path", path),
			attribute.String("status", strconv.Itoa(status)),
		),
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
