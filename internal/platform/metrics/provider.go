// Package metrics initializes metrics infrastructure.
package metrics

import (
	"fmt"

	otelprometheus "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// InitMeterProvider creates an OpenTelemetry meter provider backed by the Prometheus exporter.
func InitMeterProvider() (*sdkmetric.MeterProvider, error) {
	exporter, err := otelprometheus.New()
	if err != nil {
		return nil, fmt.Errorf("create prometheus exporter: %w", err)
	}

	return sdkmetric.NewMeterProvider(sdkmetric.WithReader(exporter)), nil
}
