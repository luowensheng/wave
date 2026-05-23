# Outbox-backed delivery

Guarantee a webhook or notification eventually lands at its
destination, even if the network's flaky or the consumer is briefly
down. The outbox is durable: pending sends survive Wave restarts.

## How it works

```
1. Your code inserts a row into the outbox table.
2. A background worker drains the table, POSTing each row to its URL.
3. Failures are retried with exponential backoff (configurable).
4. After N attempts, the row moves to the DLQ (dead-letter queue).
5. `wave outbox replay` re-queues a DLQ row.
```

## YAML

```yaml
default:
  port: 8080

# Enable the outbox. SQLite-backed (created if missing).
outbox_db: ./outbox.db

storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      orders:
        columns:
          - id      INTEGER PRIMARY KEY AUTOINCREMENT
          - total   INTEGER NOT NULL
          - status  TEXT NOT NULL DEFAULT 'new'
          - at      TEXT NOT NULL DEFAULT (datetime('now'))

routes:
  # Place an order; queue a downstream notification atomically.
  - path: /orders
    method: POST
    type: storage-access
    inputs:
      - { name: total, source: body, type: int, required: true, min: 1 }
    storage-access:
      source: app
      execute: |
        INSERT INTO orders(total) VALUES ({{total}});
        INSERT INTO _wave_outbox(url, headers, body, status)
          VALUES (
            'https://hooks.example.com/orders',
            '{"Content-Type":"application/json"}',
            json_object('order_id', last_insert_rowid(), 'total', {{total}}),
            'pending'
          );
        SELECT last_insert_rowid() AS id
      output_template: '{"id": {{.Data.id}}}'
```

The outbox table (`_wave_outbox`) is created automatically when
`outbox_db:` is set. Insert into it from any SQL.

## Operate it with the CLI

```sh
# See what's pending / in DLQ
wave outbox list --db ./outbox.db
# live queue: 3
# DLQ:        1

# Inspect DLQ
wave outbox dlq --db ./outbox.db
# 42  https://hooks.example.com/orders  attempts=5  err="connection refused"

# Replay one entry
wave outbox replay --db ./outbox.db --id 42

# Or drain the entire DLQ
wave outbox replay --db ./outbox.db --all
```

## Why this beats fire-and-forget

A naive webhook handler:

```sh
# POST /webhook → POST downstream → return
```

…silently loses notifications when the downstream is down. The
outbox decouples them: the producer commits atomically to its own
storage AND the queue; the consumer-side worker takes care of
delivery with retries.

This is the "transactional outbox" pattern.

## Variations

- **Webhook receiver → outbox**: combine with
  [Stripe webhooks](/cookbook/stripe-webhooks) so every received
  event is replicated downstream durably.
- **Different consumer per URL**: each outbox row carries its own
  URL and headers. One outbox, many destinations.
- **Backoff tuning**: configure under `outbox:` in the top-level
  config (max attempts, base/jitter).

## Production checklist

- [ ] **Monitor DLQ count** via Prometheus (`wave_outbox_dlq_total`).
- [ ] **Set up an alert** when the live queue grows unboundedly
      (consumer can't keep up).
- [ ] **Replay scripts** in CI/CD — when deploying a fix, drain
      the DLQ as a release step.
- [ ] **Two outbox DBs** for prod/staging so test traffic doesn't
      mix with real.

## See also

- Demos: [`outbox-reliability-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/outbox-reliability-demo),
  [`event-fanout-hub`](https://github.com/luowensheng/wave/tree/main/examples/apps/event-fanout-hub)
- [Schedule a cron job](/cookbook/schedule) — pair with a periodic
  reconciliation task that scans for missed sends.
