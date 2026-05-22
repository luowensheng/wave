# outbox-reliability-demo

A bank-transfer-shaped write that demonstrates the outbox pattern: the
publish AND the forward-to-downstream are both governed by the durable
outbox, so the side-effect can't be lost on a downstream blip.

## What it shows off

- Top-level `outbox_db:` enabling the SQLite-backed durable outbox.
- `stream-publish.forward_url` routes through the outbox automatically.
- Worker drains in background, retries on failure, dead-letters after N.
- `inputs:` validation for typed amount + account names.

## Data flow

```
POST /transfer
  └─ stream-publish: fan SSE event + enqueue outbox delivery
        └─ outbox worker → POST /internal/ledger → INSERT INTO transfers
                                                           │
GET /transfers ◄───────────────────────────── reads from transfers table
```

The server points `DOWNSTREAM_URL` at itself (`/internal/ledger`) by
default, so the full loop works with zero extra config.

## Run

```sh
# No extra env needed — DOWNSTREAM_URL defaults to the local ledger route.
wave serve examples/apps/outbox-reliability-demo/server.yaml --port 8610

# Submit a transfer
curl -X POST http://127.0.0.1:8610/transfer \
  -H 'Content-Type: application/json' \
  -d '{"from_account":"A","to_account":"B","amount_cents":4200}'

# Wait a moment for the outbox worker to deliver, then read back rows
curl http://127.0.0.1:8610/transfers

# Watch live SSE events in a separate terminal
curl -N http://127.0.0.1:8610/events/transfers
```

## Why outbox > direct call

If we POSTed straight to the downstream from the request handler:
- A downstream 5xx would either fail the user request or silently drop.
- A process crash between the DB write and the POST loses the event.

With the outbox: the event is enqueued atomically. Delivery is the
worker's problem; restarts don't matter. The DLQ (`outbox.db`) is your
audit trail of what failed permanently.

To prove it: temporarily make `/internal/ledger` return 500 and watch
the outbox retry; restore it and the rows appear automatically.
