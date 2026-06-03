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

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/http/middleware"
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

func (f *fakeMetricsRecorder) RegisterEmailChannelDepth(func() int) error {
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

func TestOpenTelemetryRecorderRecordsHTTPMetricsAndEmailDepth(t *testing.T) {
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	recorder, err := middleware.NewOpenTelemetryRecorder(provider.Meter("middleware-test"))
	if err != nil {
		t.Fatalf("new OpenTelemetry recorder: %v", err)
	}

	depth := 7
	if err := recorder.RegisterEmailChannelDepth(func() int { return depth }); err != nil {
		t.Fatalf("register email channel depth: %v", err)
	}

	recorder.RecordHTTPRequest(context.Background(), http.MethodPatch, "/subscriptions/123", http.StatusAccepted, 1500*time.Millisecond)

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("collect metrics: %v", err)
	}

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

	emailDepthMetric := findMetric(t, metrics, "email_channel_depth")
	if emailDepthMetric.Unit != "{email}" {
		t.Errorf("unexpected gauge unit: %q", emailDepthMetric.Unit)
	}

	emailDepth, ok := emailDepthMetric.Data.(metricdata.Gauge[int64])
	if !ok {
		t.Fatalf("expected email_channel_depth to be an int64 gauge, got %T", emailDepthMetric.Data)
	}
	if len(emailDepth.DataPoints) != 1 {
		t.Fatalf("expected 1 gauge data point, got %d", len(emailDepth.DataPoints))
	}
	if emailDepth.DataPoints[0].Value != int64(depth) {
		t.Errorf("expected gauge value %d, got %d", depth, emailDepth.DataPoints[0].Value)
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
