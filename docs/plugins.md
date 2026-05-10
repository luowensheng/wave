# Plugins, Connections & Stream-Publish

Wave supports two new top-level YAML blocks (`plugins:` and
`connections:`) and two new route types (`type: plugin` and
`type: stream-publish`) for extending behavior without rebuilding the
binary and for fanning real-time events out over SSE.

> **`type: process` is unchanged and preserved.** Shell-style instant
> APIs (e.g. piping through `jq`, `awk`, `curl` to expose a quick
> output as HTTP) keep working exactly as before. `type: plugin` is
> *additive* — pick `process` for one-shot shell scripts and `plugin`
> when you want a JSON-in/JSON-out RPC contract with retries, metrics,
> and pluggable transports.

## Quick example

```yaml
plugins:
  echo:
    transport: process     # or http (gRPC, WASM are stubs in this build)
    command: "./echo"
    timeout: 5s

connections:
  payments:
    type: sse
    subscribe_path: /events/payments
    buffer_size: 32
    keep_alive_interval: 15s
    subscribe_cors_origins: ["*"]
    # Optional: subscribe_auth: ["user_jwt"]

routes:
  - path: /echo
    method: POST
    type: plugin
    plugin:
      name: echo
      trigger_key: hello
      response_output:
        echoed: response.echoed

  - path: /webhooks/test
    method: POST
    type: stream-publish
    stream-publish:
      connection: payments
      route_id: payment_events
      event_type: payment
      output:
        payment_id: response.id
        amount: response.amount
      static_meta:
        source: stripe
    # Per-route auth + IP allowlist work as usual:
    auth: ["stripe_signature"]
    ip_whitelist: ["54.187.174.169/32"]
```

A frontend then subscribes with no extra route config:

```js
const events = new EventSource('/events/payments')
events.addEventListener('payment', e => {
  const d = JSON.parse(e.data)
  console.log(d.payment_id, d.amount, d.source)
})
```

A discovery endpoint at `GET /api/streams.json` lists every
`stream-publish` route with its `route_id`, publish path, and the
`subscribe_path` of its connection — frontends can fetch this once and
avoid hardcoding endpoints.

## Plugin contract

Every transport (subprocess, HTTP, gRPC stub, WASM stub) implements the
same JSON shape:

**Request → plugin**
```json
{
  "trigger_key": "verify",
  "metadata": {"route_path":"/login","method":"POST","remote_ip":"10.0.0.1"},
  "headers":  {"Authorization":"Bearer ..."},
  "cookies":  {"session":"abc"},
  "query":    {"redirect":"/"},
  "body":     {...}
}
```

**Response ← plugin**
```json
{
  "status": 200,
  "headers": {"Set-Cookie":"easy_session=...; HttpOnly"},
  "body":    {"success": true, "user": {"uid":"..."}}
}
```

`response_output` runs as a JSON-path whitelist over the response,
wrapped under `response.*` so the spec example
`success: response.success` works as written. Both dot
(`response.user.uid`) and bracket (`[response][user][uid]`) syntax are
supported. Missing paths are silently dropped.

## Field filtering

`stream-publish.output` is a whitelist: the broadcast event only
contains keys you map. `static_meta` injects constants (always wins on
key collisions). When `output` is omitted the raw payload is forwarded
minus any `exclude_fields`.

## Subscribers

For every entry in `connections:` the server auto-registers
`GET <subscribe_path>` as an SSE endpoint. Behavior:

- `keep_alive_interval` writes `:ping` comments to defeat idle timeouts.
- `buffer_size` controls both per-subscriber channel depth and the late
  joiner replay ring buffer (so reconnecting clients don't miss the
  most recent N events).
- `max_clients` caps concurrent subscribers; over-cap returns 503.
- Slow subscribers do **not** block the publisher — events for that
  subscriber are dropped onto the floor and counted.
- `subscribe_cors_origins` makes the endpoint usable from a browser
  app on a different origin and answers `OPTIONS` preflights.
- `subscribe_auth: [...]` reuses the same auth middleware as normal
  routes.

`type: ws` and `type: auto` are accepted in config but currently fall
back to SSE — WebSocket transport is the next planned increment.

## Validation

```
wave validate path/to/server.yaml
```

Checks that every plugin / connection / route is well-formed and that
every plugin route's `name` and stream-publish route's `connection`
resolve in their respective registries. Exits 0 on success, 1 on the
first error — suitable for CI gates.

## Built-in operational endpoints

The server registers these regardless of config:

- `GET /healthz` — liveness (always 200 once the process is up)
- `GET /readyz`  — readiness (503 until `Start` finishes wiring)
- `GET /version` — the build's version string
- `GET /api/streams.json` — registered stream-publish routes (only when at least one exists)

Outer middleware: request-ID propagation (echoes or generates
`X-Request-Id`), security headers (HSTS over TLS, X-Content-Type-Options,
X-Frame-Options=DENY, Referrer-Policy, Permissions-Policy), 16 MiB body
size cap. Graceful shutdown is wired to SIGINT/SIGTERM with a 10s drain.
