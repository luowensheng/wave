# Forward Stripe webhooks

Receive Stripe events with signature verification, persist them, and
fan them out to your frontend via SSE.

## YAML

```yaml
default:
  port: 8080

env:
  STRIPE_WEBHOOK_SECRET: { description: "whsec_… from Stripe dashboard" }

connections:
  payments:
    type: sse
    subscribe_path: /events/payments
    buffer_size: 128

storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      stripe_events:
        columns:
          - id      INTEGER PRIMARY KEY AUTOINCREMENT
          - kind    TEXT NOT NULL
          - payload TEXT NOT NULL
          - at      TEXT NOT NULL DEFAULT (datetime('now'))

routes:
  # Stripe POSTs here; Wave verifies the HMAC signature, rejects
  # any request that fails. Verified requests are persisted and
  # republished to the SSE broker.
  - path: /webhooks/stripe
    method: POST
    type: stream-publish
    expected_content_type: application/json
    webhook_sig:
      provider: stripe
      secret: "${env:STRIPE_WEBHOOK_SECRET}"
      tolerance_sec: 300            # 5-minute replay window
    stream-publish:
      connection: payments
      event_type_from: type          # use the Stripe event type
      store:
        source: app
        execute: |
          INSERT INTO stripe_events(kind, payload)
          VALUES ({{type}}, {{body_raw}})
```

## Try it

```sh
# 1. Get a webhook secret from the Stripe dashboard
export STRIPE_WEBHOOK_SECRET=whsec_…

# 2. Run Wave
wave serve server.yaml --port 8080

# 3. Use the Stripe CLI to forward events
stripe listen --forward-to localhost:8080/webhooks/stripe

# 4. Watch them arrive
curl -N http://localhost:8080/events/payments
```

## What's wired for you

- **Signature verification**: Stripe's `Stripe-Signature` header is
  HMAC-SHA256-verified against your `STRIPE_WEBHOOK_SECRET`.
  Mismatches return 401 before the handler runs.
- **Replay protection**: `tolerance_sec` rejects timestamps older
  than the window (defends against replayed captures).
- **Persistence**: every verified event lands in `stripe_events`
  for audit.
- **Live broadcast**: connected SSE clients see the event the
  instant it lands.

## Other webhook providers

The `webhook_sig:` block supports:

| `provider` | Header read | Algorithm |
|---|---|---|
| `stripe` | `Stripe-Signature` | HMAC-SHA256 with timestamp |
| `github` | `X-Hub-Signature-256` | HMAC-SHA256 |
| `slack` | `X-Slack-Signature` + `X-Slack-Request-Timestamp` | HMAC-SHA256 |
| `generic` | configurable header + algorithm | HMAC-SHA256 / SHA1 |

Switch by changing `provider:` and the matching `secret:`. See
[`webhook-signature-verify`](https://github.com/luowensheng/wave/tree/main/examples/apps/webhook-signature-verify)
for a generic-HMAC tour.

## Production checklist

- [ ] **Pin the Stripe API version** in your dashboard config.
- [ ] **Replay tolerance** ≤ 5 minutes (Stripe's recommendation).
- [ ] **Outbox for downstream side-effects** — if a webhook should
      result in an email/notification, queue via the
      [outbox](/cookbook/outbox), not synchronously from the
      webhook handler.
- [ ] **Idempotency** — Stripe may retry. Index `stripe_events.kind`
      on event id (Stripe gives you one) and dedupe.
- [ ] **Rate-limit the route** if you expect bursts — see
      [Rate-limit an endpoint](/cookbook/rate-limit).

## See also

- Demos: [`stripe-webhook-receiver`](https://github.com/luowensheng/wave/tree/main/examples/apps/stripe-webhook-receiver),
  [`github-webhook`](https://github.com/luowensheng/wave/tree/main/examples/apps/github-webhook),
  [`webhook-signature-verify`](https://github.com/luowensheng/wave/tree/main/examples/apps/webhook-signature-verify)
- [Outbox-backed delivery](/cookbook/outbox)
- [Stream events with SSE](/cookbook/sse)
