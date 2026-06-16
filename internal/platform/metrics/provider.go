// Package metrics initializes metrics infrastructure.
package metrics

import (
	"fmt"

	"github.com/prometheus/client_golang/prometheus"
	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// InitMeterProvider creates an OpenTelemetry meter provider backed by the Prometheus exporter.
func InitMeterProvider() (*sdkmetric.MeterProvider, error) {
	return NewMeterProvider(prometheus.DefaultRegisterer)
}

// NewMeterProvider creates an OpenTelemetry meter provider backed by the
// Prometheus exporter registered with registerer.
func NewMeterProvider(registerer prometheus.Registerer) (*sdkmetric.MeterProvider, error) {
	exporter, err := otelprometheus.New(otelprometheus.WithRegisterer(registerer))
	if err != nil {
		return nil, fmt.Errorf("create prometheus exporter: %w", err)
	}

	return sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter)), nil
}
