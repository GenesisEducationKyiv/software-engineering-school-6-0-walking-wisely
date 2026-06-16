//go:build integration

package main

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	dto "github.com/prometheus/client_model/go"
	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/http/middleware"
	platformlogger "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/logger"
	platformmetrics "github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/metrics"
)

func TestIntegration_HTTPMetricsAreExposedOnMetricsEndpoint(t *testing.T) {
	registry := prometheus.NewRegistry()
	provider, err := platformmetrics.NewMeterProvider(registry)
	if err != nil {
		t.Fatalf("new meter provider: %v", err)
	}
	defer func() {
		if err := provider.Shutdown(t.Context()); err != nil {
			t.Fatalf("shutdown meter provider: %v", err)
		}
	}()

	recorder, err := middleware.NewOpenTelemetryRecorder(provider.Meter("cmd-server-test"))
	if err != nil {
		t.Fatalf("new metrics recorder: %v", err)
	}
	if err := recorder.RegisterEmailChannelDepth(func() int { return 3 }); err != nil {
		t.Fatalf("register email channel depth: %v", err)
	}

	gwMux := newGatewayMux()
	if err := registerGatewayRoutes(gwMux); err != nil {
		t.Fatalf("register gateway routes: %v", err)
	}
	handler := newHTTPHandler(
		gwMux,
		promhttp.HandlerFor(registry, promhttp.HandlerOpts{}),
		recorder,
		platformlogger.NoopLogger{},
	)

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := http.Get(server.URL + "/health?email=hidden@example.com")
	if err != nil {
		t.Fatalf("request health route: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected health status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	metrics := scrapeMetrics(t, server.URL+"/metrics")

	requestsTotal := findMetricFamily(t, metrics, "http_requests_total")
	requestMetric := findMetricWithLabels(t, requestsTotal, map[string]string{
		"method": "GET",
		"path":   "/health",
		"status": "200",
	})
	if got := requestMetric.GetCounter().GetValue(); got != 1 {
		t.Fatalf("expected http_requests_total value 1, got %f", got)
	}

	duration := findMetricFamily(t, metrics, "http_request_duration_seconds")
	durationMetric := findMetricWithLabels(t, duration, map[string]string{
		"method": "GET",
		"path":   "/health",
	})
	if got := durationMetric.GetHistogram().GetSampleCount(); got != 1 {
		t.Fatalf("expected duration histogram sample count 1, got %d", got)
	}

	emailDepth := findMetricFamily(t, metrics, "email_channel_depth")
	emailDepthMetric := findMetricWithLabels(t, emailDepth, map[string]string{})
	if got := emailDepthMetric.GetGauge().GetValue(); got != 3 {
		t.Fatalf("expected email channel depth 3, got %f", got)
	}

	if metricWithLabels(requestsTotal, map[string]string{"path": "/metrics"}) != nil {
		t.Fatalf("did not expect /metrics requests to be recorded by app metrics middleware")
	}
}

func scrapeMetrics(t *testing.T, url string) map[string]*dto.MetricFamily {
	t.Helper()

	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("scrape metrics: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected metrics status %d, got %d", http.StatusOK, resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read metrics body: %v", err)
	}

	parser := expfmt.NewTextParser(model.UTF8Validation)
	metrics, err := parser.TextToMetricFamilies(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("parse metrics body: %v\n%s", err, string(body))
	}

	return metrics
}

func findMetricFamily(t *testing.T, metrics map[string]*dto.MetricFamily, name string) *dto.MetricFamily {
	t.Helper()

	metricFamily, ok := metrics[name]
	if !ok {
		t.Fatalf("metric family %q not found", name)
	}

	return metricFamily
}

func findMetricWithLabels(t *testing.T, family *dto.MetricFamily, labels map[string]string) *dto.Metric {
	t.Helper()

	metric := metricWithLabels(family, labels)
	if metric == nil {
		t.Fatalf("metric %q with labels %v not found", family.GetName(), labels)
	}

	return metric
}

func metricWithLabels(family *dto.MetricFamily, labels map[string]string) *dto.Metric {
	for _, metric := range family.GetMetric() {
		if metricHasLabels(metric, labels) {
			return metric
		}
	}

	return nil
}

func metricHasLabels(metric *dto.Metric, labels map[string]string) bool {
	for wantName, wantValue := range labels {
		found := false
		for _, label := range metric.GetLabel() {
			if label.GetName() == wantName && label.GetValue() == wantValue {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}
