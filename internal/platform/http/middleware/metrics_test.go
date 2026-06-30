package middleware_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/http/middleware"
)

type metricsCall struct {
	ctx      context.Context
	method   string
	path     string
	status   int
	duration time.Duration
}

type fakeMetricsRecorder struct {
	calls []metricsCall
}

func (f *fakeMetricsRecorder) RecordHTTPRequest(ctx context.Context, method, path string, status int, duration time.Duration) {
	f.calls = append(f.calls, metricsCall{
		ctx:      ctx,
		method:   method,
		path:     path,
		status:   status,
		duration: duration,
	})
}

func (f *fakeMetricsRecorder) RegisterOutboxMetrics(middleware.OutboxMetricsSnapshotFunc) error {
	return nil
}

func (f *fakeMetricsRecorder) RegisterGitHubAvailability(func() bool) error {
	return nil
}

func (f *fakeMetricsRecorder) RegisterGitHubRateLimitRemaining(func() int) error {
	return nil
}

func TestMetricsRecordsHTTPRequest(t *testing.T) {
	recorder := &fakeMetricsRecorder{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	})

	metricsHandler := middleware.Metrics(handler, recorder)

	req := httptest.NewRequest(http.MethodPost, "/subscriptions?email=user@example.com", http.NoBody)
	rec := httptest.NewRecorder()

	metricsHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("expected response status %d, got %d", http.StatusCreated, rec.Code)
	}
	if rec.Body.String() != "created" {
		t.Fatalf("expected response body %q, got %q", "created", rec.Body.String())
	}

	if len(recorder.calls) != 1 {
		t.Fatalf("expected 1 metrics call, got %d", len(recorder.calls))
	}

	call := recorder.calls[0]
	if call.ctx != req.Context() {
		t.Errorf("expected request context to be passed to recorder")
	}
	if call.method != http.MethodPost {
		t.Errorf("expected method %q, got %q", http.MethodPost, call.method)
	}
	if call.path != "/subscriptions" {
		t.Errorf("expected path %q, got %q", "/subscriptions", call.path)
	}
	if call.status != http.StatusCreated {
		t.Errorf("expected status %d, got %d", http.StatusCreated, call.status)
	}
}

func TestMetricsRecordsOKWhenHandlerDoesNotWriteHeader(t *testing.T) {
	recorder := &fakeMetricsRecorder{}
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	metricsHandler := middleware.Metrics(handler, recorder)

	req := httptest.NewRequest(http.MethodGet, "/health", http.NoBody)
	rec := httptest.NewRecorder()

	metricsHandler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected response status %d, got %d", http.StatusOK, rec.Code)
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("expected 1 metrics call, got %d", len(recorder.calls))
	}
	if recorder.calls[0].status != http.StatusOK {
		t.Errorf("expected recorded status %d, got %d", http.StatusOK, recorder.calls[0].status)
	}
}

func TestOpenTelemetryRecorderRecordsHTTPMetricsAndObservableGauges(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder, err := middleware.NewOpenTelemetryRecorder(provider.Meter("middleware-test"))
	if err != nil {
		t.Fatalf("new OpenTelemetry recorder: %v", err)
	}

	if err := recorder.RegisterOutboxMetrics(func(context.Context) (int64, float64, int64, int64, error) {
		return 7, 11.5, 2, 1, nil
	}); err != nil {
		t.Fatalf("register outbox metrics: %v", err)
	}
	if err := recorder.RegisterGitHubAvailability(func() bool { return true }); err != nil {
		t.Fatalf("register github availability: %v", err)
	}
	if err := recorder.RegisterGitHubRateLimitRemaining(func() int { return 42 }); err != nil {
		t.Fatalf("register github rate limit remaining: %v", err)
	}

	recorder.RecordHTTPRequest(context.Background(), http.MethodPatch, "/subscriptions/123", http.StatusAccepted, 1500*time.Millisecond)

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}

	requestsMetric := findMetric(t, metrics, "http_requests_total")
	if requestsMetric.Description != "Total HTTP requests handled." {
		t.Errorf("unexpected counter description: %q", requestsMetric.Description)
	}
	if requestsMetric.Unit != "{request}" {
		t.Errorf("unexpected counter unit: %q", requestsMetric.Unit)
	}

	requests, ok := requestsMetric.Data.(metricdata.Sum[int64])
	if !ok {
		t.Fatalf("expected http_requests_total to be an int64 sum, got %T", requestsMetric.Data)
	}
	if len(requests.DataPoints) != 1 {
		t.Fatalf("expected 1 counter data point, got %d", len(requests.DataPoints))
	}
	if requests.DataPoints[0].Value != 1 {
		t.Errorf("expected counter value 1, got %d", requests.DataPoints[0].Value)
	}
	assertAttribute(t, requests.DataPoints[0].Attributes, "method", http.MethodPatch)
	assertAttribute(t, requests.DataPoints[0].Attributes, "path", "/subscriptions/123")
	assertAttribute(t, requests.DataPoints[0].Attributes, "status", "202")

	durationMetric := findMetric(t, metrics, "http_request_duration_seconds")
	if durationMetric.Description != "HTTP request latency." {
		t.Errorf("unexpected histogram description: %q", durationMetric.Description)
	}
	if durationMetric.Unit != "s" {
		t.Errorf("unexpected histogram unit: %q", durationMetric.Unit)
	}

	duration, ok := durationMetric.Data.(metricdata.Histogram[float64])
	if !ok {
		t.Fatalf("expected http_request_duration_seconds to be a float64 histogram, got %T", durationMetric.Data)
	}
	if len(duration.DataPoints) != 1 {
		t.Fatalf("expected 1 histogram data point, got %d", len(duration.DataPoints))
	}
	if duration.DataPoints[0].Count != 1 {
		t.Errorf("expected histogram count 1, got %d", duration.DataPoints[0].Count)
	}
	if duration.DataPoints[0].Sum != 1.5 {
		t.Errorf("expected histogram sum 1.5, got %f", duration.DataPoints[0].Sum)
	}
	assertAttribute(t, duration.DataPoints[0].Attributes, "method", http.MethodPatch)
	assertAttribute(t, duration.DataPoints[0].Attributes, "path", "/subscriptions/123")
	assertAttribute(t, duration.DataPoints[0].Attributes, "status", "202")

	outboxPendingMetric := findMetric(t, metrics, "outbox_pending_count")
	if outboxPendingMetric.Unit != "{event}" {
		t.Errorf("unexpected outbox pending gauge unit: %q", outboxPendingMetric.Unit)
	}

	outboxPending, ok := outboxPendingMetric.Data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatalf("expected outbox_pending_count to be an int64 gauge, got %T", outboxPendingMetric.Data)
	}
	if len(outboxPending.DataPoints) != 1 {
		t.Fatalf("expected 1 gauge data point, got %d", len(outboxPending.DataPoints))
	}
	if outboxPending.DataPoints[0].Value != 7 {
		t.Errorf("expected outbox pending value 7, got %d", outboxPending.DataPoints[0].Value)
	}

	outboxOldestMetric := findMetric(t, metrics, "outbox_oldest_pending_age_seconds")
	outboxOldest, ok := outboxOldestMetric.Data.(metricdata.Gauge[float64])
	if !ok {
		t.Fatalf("expected outbox_oldest_pending_age_seconds to be a float64 gauge, got %T", outboxOldestMetric.Data)
	}
	if len(outboxOldest.DataPoints) != 1 || outboxOldest.DataPoints[0].Value != 11.5 {
		t.Fatalf("unexpected outbox oldest age datapoints: %#v", outboxOldest.DataPoints)
	}

	outboxRetryMetric := findMetric(t, metrics, "outbox_retry_count")
	outboxRetry, ok := outboxRetryMetric.Data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatalf("expected outbox_retry_count to be an int64 gauge, got %T", outboxRetryMetric.Data)
	}
	if len(outboxRetry.DataPoints) != 1 || outboxRetry.DataPoints[0].Value != 2 {
		t.Fatalf("unexpected outbox retry datapoints: %#v", outboxRetry.DataPoints)
	}

	outboxFailedMetric := findMetric(t, metrics, "outbox_failed_count")
	outboxFailed, ok := outboxFailedMetric.Data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatalf("expected outbox_failed_count to be an int64 gauge, got %T", outboxFailedMetric.Data)
	}
	if len(outboxFailed.DataPoints) != 1 || outboxFailed.DataPoints[0].Value != 1 {
		t.Fatalf("unexpected outbox failed datapoints: %#v", outboxFailed.DataPoints)
	}

	githubAvailableMetric := findMetric(t, metrics, "github_available")
	if githubAvailableMetric.Unit != "{state}" {
		t.Errorf("unexpected github availability gauge unit: %q", githubAvailableMetric.Unit)
	}

	githubAvailable, ok := githubAvailableMetric.Data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatalf("expected github_available to be an int64 gauge, got %T", githubAvailableMetric.Data)
	}
	if len(githubAvailable.DataPoints) != 1 {
		t.Fatalf("expected 1 github availability gauge data point, got %d", len(githubAvailable.DataPoints))
	}
	if githubAvailable.DataPoints[0].Value != 1 {
		t.Errorf("expected github availability value 1, got %d", githubAvailable.DataPoints[0].Value)
	}

	githubRateLimitMetric := findMetric(t, metrics, "github_rate_limit_remaining")
	if githubRateLimitMetric.Unit != "{request}" {
		t.Errorf("unexpected github rate limit gauge unit: %q", githubRateLimitMetric.Unit)
	}

	githubRateLimit, ok := githubRateLimitMetric.Data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatalf("expected github_rate_limit_remaining to be an int64 gauge, got %T", githubRateLimitMetric.Data)
	}
	if len(githubRateLimit.DataPoints) != 1 {
		t.Fatalf("expected 1 github rate limit gauge data point, got %d", len(githubRateLimit.DataPoints))
	}
	if githubRateLimit.DataPoints[0].Value != 42 {
		t.Errorf("expected github rate limit value 42, got %d", githubRateLimit.DataPoints[0].Value)
	}
}

func findMetric(t *testing.T, metrics metricdata.ResourceMetrics, name string) metricdata.Metrics {
	t.Helper()

	for _, scopeMetrics := range metrics.ScopeMetrics {
		for _, metric := range scopeMetrics.Metrics {
			if metric.Name == name {
				return metric
			}
		}
	}

	t.Fatalf("metric %q not found", name)
	return metricdata.Metrics{}
}

func assertAttribute(t *testing.T, attrs attribute.Set, key, want string) {
	t.Helper()

	got, ok := attrs.Value(attribute.Key(key))
	if !ok {
		t.Fatalf("attribute %q not found", key)
	}
	if got.AsString() != want {
		t.Errorf("expected attribute %q to be %q, got %q", key, want, got.AsString())
	}
}
