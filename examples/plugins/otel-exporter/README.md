# otel-exporter — reference exporter-kind plugin

OTLP gRPC exporter for metrics and traces. Logs go to stderr as JSON
in v1 (the OTel logs SDK is still unstable for Go).

## Build

```sh
go build -o wave-otel .
```

## Configure

Wire in `wave.yaml`:

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
  exporters: [otel]
```

The orchestrator pushes batches every second (or every 512 samples)
to this plugin. Slow plugins drop overflow rather than blocking the
request path.

## Live test

Stand up an OTLP receiver (Jaeger / Tempo / OTel collector) on
:4317, run wave, watch metrics flow in.

## Caveats

- Histograms in `infra/observability` are simplified; full bucketed
  exposure happens here at the OTel SDK layer.
- Trace IDs from the orchestrator are advisory in v1; W3C
  traceparent threading lands in a later phase.
