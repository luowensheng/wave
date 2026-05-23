# Rate-limit an endpoint

Protect a public-facing route from abuse with a per-IP (or per-claim)
token bucket. The framework's `limits:` registry defines named
quotas; routes reference them.

## YAML

```yaml
default:
  port: 8080

# Reusable failure-handling registry. Each entry binds a "case"
# (rate_limited, body_too_large, etc.) to a response.
limits:
  rate_100rpm:
    case: rate_limited
    rps: 1.66         # 100 / 60
    burst: 20
    on_fail:
      status_code: 429
      headers: [["Retry-After", "60"], ["Content-Type", "application/json"]]
      body: '{"error":"too many requests","retry_after":60}'

  rate_10rps_by_user:
    case: rate_limited
    rps: 10
    burst: 30
    key_claim: sub       # bucket per authenticated user instead of per IP
    on_fail:
      status_code: 429
      headers: [["Retry-After", "1"], ["Content-Type", "application/json"]]
      body: '{"error":"slow down"}'

routes:
  # Public route — limit by IP
  - path: /api/public/search
    method: GET
    type: storage-access
    limits: [rate_100rpm]
    inputs:
      - { name: q, source: query, type: string, required: true, max: 100 }
    storage-access:
      source: app
      execute: "SELECT id, name FROM items WHERE name LIKE {{wrap \"%q%\"}} LIMIT 50"
      output_template: '{{toJSON .Data}}'

  # Authenticated route — limit per user
  - path: /api/user/messages
    method: POST
    auth: [app]
    limits: [rate_10rps_by_user]
    type: storage-access
    inputs:
      - { name: body, source: body, type: string, required: true }
    storage-access:
      source: app
      execute: "INSERT INTO messages(user, body) VALUES ({{getUser}}, {{body}})"
      output_template: '{"id": {{.LastInsertID}}}'
```

## Try it

```sh
wave serve server.yaml --port 8080

# Hammer the public endpoint — should start 429'ing after the burst
for i in $(seq 1 50); do
  curl -s -o /dev/null -w '%{http_code}\n' 'http://localhost:8080/api/public/search?q=x'
done | sort | uniq -c
# Expected: ~20 lines of 200, the rest 429
```

## Bucket key strategies

| `key_claim` | Bucket scope |
|---|---|
| (unset, default) | per client IP |
| `sub` | per user (OIDC subject claim) |
| `tenant_id` | per tenant |
| Any claim from the auth ID token | per claim value |

When `key_claim` is set, the limiter falls back to client IP if the
claim is missing (e.g., unauthenticated requests still get bucketed).

## Compose with other failure cases

`limits:` covers more than rate limiting. One named entry per case:

| `case` | Triggered when |
|---|---|
| `rate_limited` | bucket exhausted |
| `body_too_large` | request body exceeds `max_bytes` |
| `unauthenticated` | auth middleware rejected |
| `forbidden` | RBAC rejected |
| `invalid_inputs` | input validation failed |
| `circuit_open` | downstream circuit breaker open |
| `missing_signature` | webhook signature missing/bad |
| `error` | status-code-range error swap |

A route declares `limits: [a, b, c]` and the framework picks the
right handler per case. Cases can be redefined in cascade — the
last name listed wins per case.

## Production checklist

- [ ] Public-facing routes should always have a rate limit.
- [ ] Authenticated routes should bucket per user (or per tenant)
      — IP bucketing punishes corporate NATs.
- [ ] `burst:` ≈ peak-second traffic so legitimate users aren't
      punished by latency spikes.
- [ ] Pair with **circuit breaker** on downstream calls (`case:
      circuit_open`) so one slow backend doesn't cascade.

## See also

- Demos: [`api-gateway-rate-limited`](https://github.com/luowensheng/wave/tree/main/examples/apps/api-gateway-rate-limited),
  [`rate-limited-public-api`](https://github.com/luowensheng/wave/tree/main/examples/apps/rate-limited-public-api)
- Concepts: [Observability](/guide/concepts-observability) — the
  limiter emits Prometheus counters for accepted/rejected requests.
