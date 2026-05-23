# Schedule a cron job

In-process scheduler with `action` (what to do) + `then` (what to do
with the result). No external cron, no separate worker tier.

## What it can do

- `action: { type: api, ... }` — call an HTTP endpoint
- `action: { type: plugin, ... }` — call a plugin
- `action: { type: storage, ... }` — run SQL

…then chain sinks:

- `then: type: storage` — persist the result
- `then: type: publish` — broadcast over SSE
- `then: type: plugin` — pass to another plugin

## YAML — poll an API, save it, broadcast it

```yaml
default:
  port: 8080

connections:
  market_feed:
    type: sse
    subscribe_path: /events/market

storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      prices:
        columns:
          - id    INTEGER PRIMARY KEY AUTOINCREMENT
          - price REAL NOT NULL
          - at    TEXT NOT NULL DEFAULT (datetime('now'))

schedule:
  poll_prices:
    every: 30s
    action:
      type: api
      url: https://api.example.com/btc-usd
      method: GET
    then:
      - type: storage
        source: app
        inputs:
          price: price       # dot-path into action result
        execute: "INSERT INTO prices(price) VALUES ({{price}})"
      - type: publish
        connection: market_feed
        event_type: price_update

routes:
  # Inspect the schedule's history
  - path: /prices
    method: GET
    type: storage-access
    storage-access:
      source: app
      execute: "SELECT * FROM prices ORDER BY id DESC LIMIT 20"
      output_template: '{{toJSON .Data}}'
```

## Try it

```sh
wave serve server.yaml --port 8080

# Watch live broadcasts
curl -N http://localhost:8080/events/market

# After 30s, query history
curl http://localhost:8080/prices
```

## Daily at a wall-clock time

```yaml
schedule:
  nightly_cleanup:
    at: "03:30"           # 03:30 in the server's local timezone
    action:
      type: storage
      source: app
      execute: "DELETE FROM sessions WHERE expires_at < datetime('now')"
```

## Legacy plugin-only form (still works)

If you don't need `action + then`, the simpler shape is fine:

```yaml
schedule:
  ping_health:
    every: 1m
    plugin: healthchecker
    trigger_key: run
    body: { mode: full }
```

## Variations

- **Multiple sinks** — chain as many `then:` entries as you need.
- **Conditional sink** — push a `type: plugin` sink that decides
  whether to forward.
- **Plugin action**: `action: { type: plugin, plugin: foo }` calls
  your custom worker.
- **No persistence**: drop the storage sink. The job just publishes
  to SSE.

## Caveats

- **Not persisted across restarts.** If the server restarts at 03:29,
  the 03:30 job won't fire that day unless the server is up by 03:30.
  For durable scheduling, use a real workflow engine.
- **In-process** — fires inside the Wave server. If the host dies,
  no failover. For HA, put a single leader-elected scheduler in
  front and have it call HTTP routes on each instance.

## See also

- Demos: [`scheduled-jobs-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/scheduled-jobs-demo),
  [`cron-data-refresh`](https://github.com/luowensheng/wave/tree/main/examples/apps/cron-data-refresh)
- [Background tasks](/cookbook/background-tasks)
- [Outbox-backed delivery](/cookbook/outbox)
