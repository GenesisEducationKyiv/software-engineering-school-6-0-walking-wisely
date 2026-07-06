// Package metrics initializes metrics infrastructure.
package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/metric"
)

// httpDurationBuckets are explicit boundaries (in seconds) for the HTTP request
// duration histogram. Covers 5 ms–10 s, giving meaningful p95/p99 resolution
// for endpoints that hit a DB or make an external API call (e.g. subscribe).
var httpDurationBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5, 5.0, 10.0}

// InitMeterProvider creates an OpenTelemetry meter provider backed by the Prometheus exporter.
func InitMeterProvider() (*metric.MeterProvider, error) {
	return NewMeterProvider(prometheus.DefaultRegisterer)
}

// NewMeterProvider creates an OpenTelemetry meter provider backed by the
// Prometheus exporter registered with registerer.
func NewMeterProvider(registerer prometheus.Registerer) (*metric.MeterProvider, error) {
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registerer))
	if err != nil {
		return nil, fmt.Errorf("create prometheus exporter: %w", err)
	}

	durationView := metric.NewView(
		metric.Instrument{Name: "http_request_duration_seconds"},
		metric.Stream{
			Aggregation: metric.AggregationExplicitBucketHistogram{
				Boundaries: httpDurationBuckets,
			},
		},
	)

	return metric.NewMeterProvider(
		metric.WithReader(exporter),
		metric.WithView(durationView),
	), nil
}
