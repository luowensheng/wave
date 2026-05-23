# Stripe Checkout (one-time + subscription payments)

Take payments without writing your own card form. Stripe-hosted
Checkout handles PCI, 3D Secure, Apple/Google Pay; Wave creates the
session and listens for the webhook.

Two routes do the work:
1. `POST /api/checkout` — create a Stripe Checkout session, return its URL
2. `POST /webhooks/stripe` — receive `checkout.session.completed` and persist the order

For webhook signature verification, see the
[**Stripe webhooks recipe**](/cookbook/stripe-webhooks) — this page
focuses on the checkout half.

## What you need from Stripe first

1. Get a Secret Key from [dashboard.stripe.com/apikeys](https://dashboard.stripe.com/apikeys) (test mode is fine)
2. Create one **Product + Price** in the dashboard (or do it via the API)
3. For webhooks: create a webhook endpoint pointing at
   `https://your-domain.com/webhooks/stripe`, then copy the signing secret

## YAML — one-time payment

```yaml
default:
  port: 8080

env:
  STRIPE_SECRET_KEY:         { description: "sk_test_..." }
  STRIPE_WEBHOOK_SECRET:     { description: "whsec_..." }
  STRIPE_PRICE_ID:           { description: "price_..." }
  APP_URL:                   { description: "https://yourapp.com" }

storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      orders:
        columns:
          - id         INTEGER PRIMARY KEY AUTOINCREMENT
          - session_id TEXT NOT NULL UNIQUE
          - email      TEXT
          - amount     INTEGER
          - status     TEXT NOT NULL DEFAULT 'pending'
          - at         TEXT NOT NULL DEFAULT (datetime('now'))

requests:
  stripe_create_session:
    method: POST
    url: https://api.stripe.com/v1/checkout/sessions
    headers:
      Authorization: "Bearer ${env:STRIPE_SECRET_KEY}"
      Content-Type:  "application/x-www-form-urlencoded"
    # Form-encoded body — Stripe's API expects this format
    body: "mode=payment&line_items[0][price]={{price_id}}&line_items[0][quantity]=1&success_url=${APP_URL}/orders/success?sid={CHECKOUT_SESSION_ID}&cancel_url=${APP_URL}/orders/cancel"

routes:
  # 1. Frontend POSTs to here; Wave creates a Stripe session, returns its URL.
  #    Frontend then window.location = response.url.
  - path: /api/checkout
    method: POST
    type: fetch
    inputs:
      - { name: price_id, source: body, type: string, default: "${env:STRIPE_PRICE_ID}" }
    fetch:
      request: stripe_create_session
      response:
        output_template: '{"url": "{{.Body.url}}", "id": "{{.Body.id}}"}'

  # 2. Stripe POSTs the event when the customer pays.
  #    HMAC verified before the handler runs.
  - path: /webhooks/stripe
    method: POST
    type: storage-access
    expected_content_type: application/json
    webhook_sig:
      provider: stripe
      secret: "${env:STRIPE_WEBHOOK_SECRET}"
      tolerance_sec: 300
    inputs:
      - { name: type, source: body, type: string, required: true }
      - { name: data, source: body, type: object, required: true }
    storage-access:
      source: app
      execute: |
        INSERT INTO orders(session_id, email, amount, status)
        VALUES (
          json_extract({{toJSON .data}}, '$.object.id'),
          json_extract({{toJSON .data}}, '$.object.customer_email'),
          json_extract({{toJSON .data}}, '$.object.amount_total'),
          CASE WHEN {{type}} = 'checkout.session.completed' THEN 'paid' ELSE 'pending' END
        )
        ON CONFLICT(session_id) DO UPDATE SET
          status = excluded.status,
          email  = COALESCE(excluded.email, email),
          amount = COALESCE(excluded.amount, amount)
      output_template: '{"received": true}'

  # 3. Order-success landing page (Stripe redirects here after payment)
  - path: /orders/success
    method: GET
    type: storage-access
    inputs:
      - { name: sid, source: query, type: string, required: true }
    storage-access:
      source: app
      execute: "SELECT id, status, amount, email FROM orders WHERE session_id = {{sid}} LIMIT 1"
      if_empty_status: 404
      response_content_type: text/html
      output_template: |
        <!doctype html>
        <h1>Thanks for your order!</h1>
        <p>Status: {{.Data.status}}</p>
        <p>Amount: ${{.Data.amount}}</p>
```

## Try it

```sh
export STRIPE_SECRET_KEY=sk_test_...
export STRIPE_WEBHOOK_SECRET=whsec_...
export STRIPE_PRICE_ID=price_...
export APP_URL=http://localhost:8080

wave serve server.yaml --port 8080

# 1. Forward webhooks to local Wave (separate terminal)
stripe listen --forward-to localhost:8080/webhooks/stripe

# 2. Create a checkout session
curl -X POST http://localhost:8080/api/checkout
# {"url": "https://checkout.stripe.com/c/pay/cs_test_...", "id": "cs_test_..."}

# 3. Pay with Stripe's test card 4242 4242 4242 4242 at the returned URL
# 4. Watch the webhook arrive + the order row update
sqlite3 data.db "SELECT * FROM orders ORDER BY id DESC LIMIT 1"
```

## Subscription mode

Change `mode=payment` → `mode=subscription` in the `body:` template
and use a recurring `price_...` ID:

```yaml
requests:
  stripe_create_subscription:
    method: POST
    url: https://api.stripe.com/v1/checkout/sessions
    headers:
      Authorization: "Bearer ${env:STRIPE_SECRET_KEY}"
      Content-Type:  "application/x-www-form-urlencoded"
    body: "mode=subscription&line_items[0][price]={{price_id}}&line_items[0][quantity]=1&success_url=${APP_URL}/billing/success?sid={CHECKOUT_SESSION_ID}&cancel_url=${APP_URL}/billing/cancel&customer_email={{email}}"
```

Then handle these webhook events:
- `customer.subscription.created` — new subscription
- `customer.subscription.updated` — plan change / cancellation scheduled
- `customer.subscription.deleted` — cancellation took effect
- `invoice.paid` — successful renewal
- `invoice.payment_failed` — failed renewal

## Customer Portal (let users manage their own subscription)

```yaml
requests:
  stripe_billing_portal:
    method: POST
    url: https://api.stripe.com/v1/billing_portal/sessions
    headers:
      Authorization: "Bearer ${env:STRIPE_SECRET_KEY}"
      Content-Type:  "application/x-www-form-urlencoded"
    body: "customer={{customer_id}}&return_url=${APP_URL}/account"

routes:
  - path: /api/billing-portal
    method: POST
    auth: [app]
    type: fetch
    inputs:
      - { name: customer_id, source: body, type: string, required: true }
    fetch:
      request: stripe_billing_portal
      response:
        output_template: '{"url": "{{.Body.url}}"}'
```

## Idempotency

Stripe retries webhooks if your endpoint times out. Make sure repeats
are safe:
- The `ON CONFLICT(session_id) DO UPDATE` clause above is already
  idempotent.
- For other side effects (sending email, fulfilling an order), gate
  on a transition: `WHERE status = 'pending'` so the second delivery
  is a no-op.

## Production checklist

- [ ] **Test mode keys** in dev (`sk_test_...`); **live keys** in prod
- [ ] Webhook secret matches the endpoint in the Stripe dashboard
- [ ] **Replay tolerance** ≤ 5 minutes (Stripe's recommendation)
- [ ] Every webhook event you care about handled (no silent drops)
- [ ] Persist `stripe_event_id` and dedupe on it (defense in depth)
- [ ] Side effects (emails, fulfillment) go through the
      [outbox](/cookbook/outbox), not synchronously from the
      webhook handler
- [ ] Customer Portal route is **`auth: [app]` + scoped to the
      caller's `customer_id`** — never trust a body-provided id

## See also

- [Stripe webhooks recipe](/cookbook/stripe-webhooks) — signature verification + persistence
- [Send transactional email](/cookbook/send-email) — order confirmations
- [Outbox-backed delivery](/cookbook/outbox) — durable fulfillment events
- [Audit log every mutation](/cookbook/audit-log) — financial events should always be audited
