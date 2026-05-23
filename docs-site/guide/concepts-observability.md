# Observability

Wave ships with the four pillars wired in: metrics, traces, logs, and
an audit channel. They're on by default; you opt out, not in.

## Health endpoints

Always registered, no config required:

| Endpoint | Returns |
|---|---|
| `GET /healthz` | 200 once boot is complete |
| `GET /readyz` | 503 until all storages, plugins, and connections are reachable |
| `GET /version` | binary version + commit (set via ldflags) |

Use these as liveness/readiness probes:

```yaml
# Kubernetes
livenessProbe:  { httpGet: { path: /healthz, port: 8080 } }
readinessProbe: { httpGet: { path: /readyz,  port: 8080 } }
```

## Prometheus metrics

`GET /metrics` exposes the Prometheus exposition format. Counters
and histograms emitted out of the box:

| Metric | Labels | What it counts |
|---|---|---|
| `wave_requests_total` | route, method, status | per-route request count |
| `wave_request_duration_seconds` | route, method | latency histogram |
| `wave_rate_limit_rejects_total` | route | requests dropped by limiter |
| `wave_circuit_open_total` | route | requests rejected by open circuit |
| `wave_storage_query_duration_seconds` | source, op | SQL latency |
| `wave_plugin_call_duration_seconds` | plugin, trigger | plugin RPC latency |
| `wave_outbox_pending` | — | live outbox queue size |
| `wave_outbox_dlq_total` | — | dead-letter count |
| `wave_sse_subscribers` | broker | connected SSE clients |

Scrape with:

```yaml
# prometheus.yml
scrape_configs:
  - job_name: wave
    static_configs:
      - targets: ['wave:8080']
```

## OpenTelemetry traces

Spans emitted automatically per route, including downstream HTTP
calls and SQL queries. Configure the OTLP exporter:

```yaml
# server.yaml
otel:
  endpoint: "${env:OTEL_EXPORTER_OTLP_ENDPOINT}"   # e.g. http://otel-collector:4318
  service_name: my-wave-app
  sample_rate: 0.1                                   # 10% sampling
```

See [`otel-tracing-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/otel-tracing-demo)
for a full pipeline with Jaeger.

## Structured logs

Logs are JSON-formatted by default. Each request emits:

```jsonc
{
  "ts": "2026-05-23T09:00:00.123Z",
  "level": "info",
  "msg": "request",
  "request_id": "abcd1234",
  "method": "POST",
  "path": "/items",
  "status": 201,
  "duration_ms": 12.4,
  "user_id": "ada",
  "ip": "192.168.1.42"
}
```

`request_id` is the same value the framework returns in the
`X-Request-ID` response header — easy correlation between log search
and a specific user-reported error.

Set `LOG_LEVEL=debug` for verbose handler-level traces. Set
`LOG_FORMAT=text` for human-readable colored output (dev only).

## Audit log

Distinct from regular logs — this is your durable, queryable
record of state-changing events. Add `audit:` to a route to emit:

```yaml
- path: /admin/users/{id}
  method: DELETE
  type: storage-access
  auth: [primary]
  audit: { action: "user.delete", target_input: id }
  storage-access: { ... }
```

The audit subsystem emits one row per mutation, with the
authenticated user, IP, timestamp, action name, and target
identifier. Persist to a dedicated storage backend via the
[audit log recipe](/cookbook/audit-log).

## Plugin-based exporter fanout

If your observability stack doesn't speak OTLP or Prometheus,
write a plugin that exports to it. Wave's observability fanout
broadcasts each event to every registered exporter plugin
concurrently — Prometheus AND your Datadog/Honeycomb/SaaS at the
same time.

See [`exporter-plugins.md`](https://github.com/luowensheng/wave/blob/main/docs/exporter-plugins.md).

## Production checklist

- [ ] Configure Prometheus scrape on `/metrics`
- [ ] OTLP endpoint set, sample rate tuned (10-25% for high volume)
- [ ] Log shipping pipeline aggregates JSON logs to your SIEM
- [ ] Audit table replicates to long-term storage (or an outbox to
      a SIEM)
- [ ] Alert on `wave_outbox_dlq_total > 0` and on `/readyz` failing
- [ ] Dashboards for: p99 request latency by route, error rate by
      status code, rate-limit rejects per minute

## See also

- Demo: [`otel-tracing-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/otel-tracing-demo)
- [Audit log recipe](/cookbook/audit-log)
- [Production checklist](/guide/deploy-checklist)
