package notify

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/GenesisEducationKyiv/software-engineering-school-6-0-walking-wisely/internal/platform/events"
)

// sagaPublishDurationBuckets covers the latency range relevant for direct gRPC calls
// (sub-millisecond) up to broker round-trips under load (seconds).
var sagaPublishDurationBuckets = []float64{
	0.0001, 0.0005, 0.001, 0.0025, 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1.0, 2.5,
}

// SagaMetrics holds OTel instruments shared across all instrumented publishers.
type SagaMetrics struct {
	publishTotal    metric.Int64Counter
	publishDuration metric.Float64Histogram
	inflight        metric.Int64UpDownCounter
}

// NewSagaMetrics registers instruments on meter.
func NewSagaMetrics(meter metric.Meter) (*SagaMetrics, error) {
	total, err := meter.Int64Counter(
		"saga_publish_total",
		metric.WithDescription("Saga publish attempts by transport and result."),
		metric.WithUnit("{publish}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create saga_publish_total: %w", err)
	}

	dur, err := meter.Float64Histogram(
		"saga_publish_duration_seconds",
		metric.WithDescription("Per-Publish call latency by transport."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(sagaPublishDurationBuckets...),
	)
	if err != nil {
		return nil, fmt.Errorf("create saga_publish_duration_seconds: %w", err)
	}

	infl, err := meter.Int64UpDownCounter(
		"saga_inflight",
		metric.WithDescription("Concurrent in-flight saga publishes by transport."),
		metric.WithUnit("{publish}"),
	)
	if err != nil {
		return nil, fmt.Errorf("create saga_inflight: %w", err)
	}

	return &SagaMetrics{
		publishTotal:    total,
		publishDuration: dur,
		inflight:        infl,
	}, nil
}

// InstrumentedPublisher wraps an events.Publisher and records per-transport metrics.
type InstrumentedPublisher struct {
	inner     events.Publisher
	transport string
	m         *SagaMetrics
}

// NewInstrumentedPublisher returns a publisher that records saga metrics labeled by transport.
func NewInstrumentedPublisher(inner events.Publisher, transport string, m *SagaMetrics) *InstrumentedPublisher {
	return &InstrumentedPublisher{inner: inner, transport: transport, m: m}
}

func (p *InstrumentedPublisher) Publish(ctx context.Context, event events.Event) error {
	transportAttr := attribute.String("transport", p.transport)

	p.m.inflight.Add(ctx, 1, metric.WithAttributes(transportAttr))
	defer p.m.inflight.Add(ctx, -1, metric.WithAttributes(transportAttr))

	start := time.Now()
	err := p.inner.Publish(ctx, event)
	elapsed := time.Since(start).Seconds()

	result := "ok"
	if err != nil {
		result = "error"
	}

	attrs := metric.WithAttributes(transportAttr, attribute.String("result", result))
	p.m.publishTotal.Add(ctx, 1, attrs)
	p.m.publishDuration.Record(ctx, elapsed, metric.WithAttributes(transportAttr))

	return err
}
