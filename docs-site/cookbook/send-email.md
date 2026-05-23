# Send transactional email (Resend / SendGrid / Postmark / Mailgun)

Send a verification code, a magic link, a notification — anything
transactional. All four major providers expose the same shape: POST
JSON to an HTTPS endpoint with an API key. Wave's `type: api` route
calls them; no SDK required.

::: tip
For **magic-link login**, you don't need this recipe — Wave's
built-in `magic-link-request` handles email delivery via
`auth_flows.smtp`. This recipe is for anything else: order
confirmations, password resets, weekly digests.
:::

## YAML — Resend

[resend.com](https://resend.com) is the simplest API and the
cheapest free tier (3000 emails/month).

```yaml
default:
  port: 8080

env:
  RESEND_API_KEY: { description: "re_… from resend.com/api-keys" }

requests:
  resend_send:
    method: POST
    url: https://api.resend.com/emails
    headers:
      Authorization: "Bearer ${env:RESEND_API_KEY}"
      Content-Type:  "application/json"
    body: |
      {
        "from":    "App <noreply@yourdomain.com>",
        "to":      ["{{to}}"],
        "subject": "{{subject}}",
        "html":    "{{html}}"
      }

routes:
  - path: /api/send-email
    method: POST
    type: fetch
    inputs:
      - { name: to,      source: body, type: email,  required: true }
      - { name: subject, source: body, type: string, required: true, min: 1, max: 200 }
      - { name: html,    source: body, type: string, required: true, min: 1 }
    fetch:
      request: resend_send                   # reference the named request above
      response:
        output_template: '{"sent": true, "id": "{{.Body.id}}"}'
```

```sh
export RESEND_API_KEY=re_...
wave serve server.yaml --port 8080

curl -X POST http://localhost:8080/api/send-email \
  -H 'Content-Type: application/json' \
  -d '{
    "to":      "ada@example.com",
    "subject": "Welcome",
    "html":    "<h1>Hello!</h1><p>Thanks for signing up.</p>"
  }'
# {"sent": true, "id": "abc123..."}
```

## YAML — SendGrid

```yaml
requests:
  sendgrid_send:
    method: POST
    url: https://api.sendgrid.com/v3/mail/send
    headers:
      Authorization: "Bearer ${env:SENDGRID_API_KEY}"
      Content-Type:  "application/json"
    body: |
      {
        "personalizations": [{"to": [{"email": "{{to}}"}]}],
        "from":    {"email": "noreply@yourdomain.com"},
        "subject": "{{subject}}",
        "content": [{"type": "text/html", "value": "{{html}}"}]
      }
```

## YAML — Postmark

```yaml
requests:
  postmark_send:
    method: POST
    url: https://api.postmarkapp.com/email
    headers:
      X-Postmark-Server-Token: "${env:POSTMARK_TOKEN}"
      Accept:        "application/json"
      Content-Type:  "application/json"
    body: |
      {
        "From":     "noreply@yourdomain.com",
        "To":       "{{to}}",
        "Subject":  "{{subject}}",
        "HtmlBody": "{{html}}"
      }
```

## YAML — Mailgun

```yaml
requests:
  mailgun_send:
    method: POST
    url: "https://api.mailgun.net/v3/${env:MAILGUN_DOMAIN}/messages"
    headers:
      Authorization: "Basic ${env:MAILGUN_BASIC_AUTH}"   # base64("api:KEY")
      Content-Type:  "application/x-www-form-urlencoded"
    body: "from=noreply@${MAILGUN_DOMAIN}&to={{to}}&subject={{subject}}&html={{html}}"
```

## Pair with a Wave route — common patterns

### Password-reset link

```yaml
routes:
  - path: /password-reset
    method: POST
    type: storage-access
    inputs:
      - { name: email, source: body, type: email, required: true }
    storage-access:
      source: app
      execute: |
        INSERT INTO password_resets(email, token, expires_at)
        VALUES ({{email}}, lower(hex(randomblob(16))), datetime('now', '+1 hour'))
        RETURNING token
    # After persistence, kick off the email via a follow-up call.
    # See: chain via type: task for fire-and-forget delivery.
```

### Order confirmation as a follow-up to a Stripe webhook

```yaml
- path: /webhooks/stripe/checkout-completed
  method: POST
  type: stream-publish
  webhook_sig:
    provider: stripe
    secret: "${env:STRIPE_WEBHOOK_SECRET}"
  stream-publish:
    connection: order_events
    store:
      # ... persist the order ...
    then:
      - type: api
        ref: resend_send
        inputs:
          to:      data.customer_email
          subject: "Order #{{order_id}} confirmed"
          html:    "<p>Thanks!</p>"
```

## Reliability: use the outbox for important sends

If the user is **waiting on a 200**, send synchronously and surface
errors immediately. If the send is a **side effect** (welcome email,
weekly digest), queue it via the [outbox](/cookbook/outbox) so:
- A flaky third-party API doesn't fail the request
- Retries happen automatically
- Failed sends land in the DLQ for `wave outbox replay`

```yaml
outbox_db: ./outbox.db

routes:
  - path: /signup
    method: POST
    type: storage-access
    inputs: [{ name: email, source: body, type: email, required: true }]
    storage-access:
      source: app
      execute: |
        INSERT INTO users(email) VALUES ({{email}});
        INSERT INTO _wave_outbox(url, headers, body, status) VALUES (
          'https://api.resend.com/emails',
          json_object('Authorization', 'Bearer ' || $RESEND_API_KEY),
          json_object('from','app@yours','to',json_array({{email}}),
                      'subject','Welcome','html','<h1>Hi</h1>'),
          'pending'
        )
      output_template: '{"id": {{.LastInsertID}}}'
```

## Production checklist

- [ ] Verify your sender domain (SPF + DKIM + DMARC). Otherwise
      Gmail/Outlook drop into spam.
- [ ] Use a dedicated subdomain (e.g. `mail.yourapp.com`) so
      transactional volume doesn't poison your marketing domain.
- [ ] **Rate-limit** `/api/send-email` and any other email-triggering
      route — see [Rate-limit recipe](/cookbook/rate-limit).
- [ ] Persist outbound sends to an `emails` table for audit.
- [ ] For high-volume: use the [outbox](/cookbook/outbox) pattern.
- [ ] **HTML-escape user input** before templating into the body.

## See also

- [Stripe webhooks recipe](/cookbook/stripe-webhooks) — pair with email send-on-event
- [Outbox-backed delivery](/cookbook/outbox) — durable email queue
- [Rate-limit an endpoint](/cookbook/rate-limit) — abuse protection
