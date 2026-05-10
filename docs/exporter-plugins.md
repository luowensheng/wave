# Exporter plugins (push-style telemetry)

wave exposes telemetry two ways:

1. **Pull** — the built-in Prometheus text exposer at `GET /metrics`.
   Always on. Zero deps.
2. **Push** — exporter-kind plugins. The orchestrator batches every
   metric / trace / log emitted by the request path and ships those
   batches to one or more plugin processes (OTel, Datadog, …).

Both run side by side. Plugins are an addition, not a replacement.

## Wiring

```yaml
plugins:
  otel:
    kind: exporter
    transport: process
    command: ./wave-otel
    env:
      OTEL_EXPORTER_OTLP_ENDPOINT: localhost:4317
      OTEL_SERVICE_NAME: my-app

observability:
  exporters: [otel]   # subset selection — empty list ⇒ all exporter-kind plugins
```

Boot validation rejects unknown names or wrong-kind references.

## Fan-out behaviour

- One drain goroutine per subscriber, bounded inbound channel
  (default 4096), batches up to 512 samples or 1 s.
- **Non-blocking.** A slow subscriber drops overflow and bumps a
  per-subscriber counter (`SubscriberDrops()` on the `Fanout`).
- A panic inside one subscriber is recovered and logged; siblings keep
  flowing.
- `Server.Stop()` (called on graceful shutdown) drains in-flight
  batches before returning.

## Histograms (v1 caveat)

The Prometheus exposer in v1 degrades histograms to a `_count`
counter plus a `_last` gauge — proper bucketed histograms need
cumulative bucket arithmetic that we deferred. Plugin exporters that
sit on real telemetry SDKs (OTel) record the full distribution
correctly because they receive raw `Sample{Type:"histogram"}` events.

## Writing your own

Implement `sdk.ExporterPlugin` and call `sdk.RunExporter(impl)` from
`main`. The SDK handles framing; you implement four methods:

```go
type ExporterPlugin interface {
    ExportMetrics(ctx, []*MetricSample) error
    ExportTraces(ctx, []*TraceSpan) error
    ExportLogs(ctx, []*LogRecord) error
    Close() error
}
```

See `examples/plugins/otel-exporter/` for the reference implementation.
