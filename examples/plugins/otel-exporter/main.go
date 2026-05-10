// Command otel-exporter is the reference exporter-kind plugin. It
// receives MetricSample / TraceSpan / LogRecord batches over JSON-RPC
// stdin/stdout (the wave SDK serve loop) and pushes them to an
// OTLP gRPC endpoint via the OpenTelemetry Go SDK.
//
// Configuration via env (the orchestrator can inject these per the
// plugin manifest):
//   OTEL_EXPORTER_OTLP_ENDPOINT  default "localhost:4317"
//   OTEL_SERVICE_NAME            default "wave"
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	sdk "wave.dev/sdk"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	otlpmetricgrpc "go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	otlptracegrpc "go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

const (
	defaultEndpoint    = "localhost:4317"
	defaultServiceName = "wave"
)

type otelExporter struct {
	mu       sync.Mutex
	meterProv *sdkmetric.MeterProvider
	tracerProv *sdktrace.TracerProvider
	meter    metric.Meter
	tracer   trace.Tracer

	// instrument cache, keyed by metric name (sample type)
	counters   map[string]metric.Float64Counter
	histograms map[string]metric.Float64Histogram
	gauges     map[string]metric.Float64Gauge
}

func newExporter(ctx context.Context) (*otelExporter, error) {
	endpoint := envOr("OTEL_EXPORTER_OTLP_ENDPOINT", defaultEndpoint)
	serviceName := envOr("OTEL_SERVICE_NAME", defaultServiceName)

	res, err := resource.New(ctx,
		resource.WithAttributes(semconv.ServiceName(serviceName)),
	)
	if err != nil {
		return nil, fmt.Errorf("resource: %w", err)
	}

	metricExp, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpoint(endpoint),
		otlpmetricgrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp,
			sdkmetric.WithInterval(10*time.Second))),
	)
	otel.SetMeterProvider(mp)

	traceExp, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(traceExp),
	)
	otel.SetTracerProvider(tp)

	return &otelExporter{
		meterProv:  mp,
		tracerProv: tp,
		meter:      mp.Meter("wave"),
		tracer:     tp.Tracer("wave"),
		counters:   map[string]metric.Float64Counter{},
		histograms: map[string]metric.Float64Histogram{},
		gauges:     map[string]metric.Float64Gauge{},
	}, nil
}

func (o *otelExporter) ExportMetrics(ctx context.Context, batch []*sdk.MetricSample) error {
	if len(batch) == 0 {
		return nil
	}
	for _, s := range batch {
		attrs := labelsToAttrs(s.Labels)
		opt := metric.WithAttributes(attrs...)
		switch s.Type {
		case "counter":
			c, err := o.getCounter(s.Name)
			if err != nil {
				return err
			}
			c.Add(ctx, s.Value, opt)
		case "histogram":
			h, err := o.getHistogram(s.Name)
			if err != nil {
				return err
			}
			h.Record(ctx, s.Value, opt)
		case "gauge":
			g, err := o.getGauge(s.Name)
			if err != nil {
				return err
			}
			g.Record(ctx, s.Value, opt)
		default:
			// Unknown type — skip silently.
		}
	}
	return nil
}

func (o *otelExporter) ExportTraces(ctx context.Context, batch []*sdk.TraceSpan) error {
	if len(batch) == 0 {
		return nil
	}
	for _, s := range batch {
		// Map to a fresh span scoped under our tracer. Caller-supplied
		// trace/span IDs are advisory in v1 — full propagation comes
		// later (Phase 5 deliberately defers W3C threading).
		_, span := o.tracer.Start(ctx, s.Name,
			trace.WithTimestamp(time.Unix(0, s.StartUnixNano)),
			trace.WithAttributes(labelsToAttrs(s.Attributes)...))
		span.End(trace.WithTimestamp(time.Unix(0, s.EndUnixNano)))
	}
	return nil
}

func (o *otelExporter) ExportLogs(_ context.Context, batch []*sdk.LogRecord) error {
	// OTel logs SDK is unstable in Go; for v1 we emit JSON to stderr so
	// the operator can still pick them up via container logs.
	for _, r := range batch {
		buf, err := json.Marshal(r)
		if err != nil {
			continue
		}
		fmt.Fprintln(os.Stderr, string(buf))
	}
	return nil
}

func (o *otelExporter) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var firstErr error
	if err := o.meterProv.Shutdown(ctx); err != nil {
		firstErr = err
	}
	if err := o.tracerProv.Shutdown(ctx); err != nil && firstErr == nil {
		firstErr = err
	}
	return firstErr
}

func (o *otelExporter) getCounter(name string) (metric.Float64Counter, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if c, ok := o.counters[name]; ok {
		return c, nil
	}
	c, err := o.meter.Float64Counter(name)
	if err != nil {
		return nil, err
	}
	o.counters[name] = c
	return c, nil
}

func (o *otelExporter) getHistogram(name string) (metric.Float64Histogram, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if h, ok := o.histograms[name]; ok {
		return h, nil
	}
	h, err := o.meter.Float64Histogram(name)
	if err != nil {
		return nil, err
	}
	o.histograms[name] = h
	return h, nil
}

func (o *otelExporter) getGauge(name string) (metric.Float64Gauge, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if g, ok := o.gauges[name]; ok {
		return g, nil
	}
	g, err := o.meter.Float64Gauge(name)
	if err != nil {
		return nil, err
	}
	o.gauges[name] = g
	return g, nil
}

func labelsToAttrs(m map[string]string) []attribute.KeyValue {
	out := make([]attribute.KeyValue, 0, len(m))
	for k, v := range m {
		out = append(out, attribute.String(k, v))
	}
	return out
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	exp, err := newExporter(ctx)
	cancel()
	if err != nil {
		fmt.Fprintln(os.Stderr, "otel-exporter: init:", err)
		os.Exit(1)
	}
	if err := sdk.RunExporter(exp); err != nil {
		fmt.Fprintln(os.Stderr, "otel-exporter:", err)
		os.Exit(1)
	}
}
