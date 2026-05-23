# Send SMS via Twilio

Send a verification code, a 2FA challenge, an outage alert — Twilio
is the workhorse. Same shape works for **Vonage**, **MessageBird**,
or any HTTPS SMS API.

## What you need from Twilio first

1. Sign up at [twilio.com](https://www.twilio.com/) (trial works fine)
2. Buy a number (or use the trial number)
3. Copy your **Account SID** and **Auth Token** from the console

## YAML

```yaml
default:
  port: 8080

env:
  TWILIO_ACCOUNT_SID: { description: "ACxxxxxxxxxxxx" }
  TWILIO_AUTH_TOKEN:  { description: "Auth token from twilio.com/console" }
  TWILIO_FROM:        { description: "Your Twilio phone number, e.g. +14155551212" }

requests:
  twilio_send:
    method: POST
    url: "https://api.twilio.com/2010-04-01/Accounts/${env:TWILIO_ACCOUNT_SID}/Messages.json"
    headers:
      Authorization: "Basic ${env:TWILIO_BASIC_AUTH}"   # base64("SID:TOKEN")
      Content-Type:  "application/x-www-form-urlencoded"
    body: "From=${env:TWILIO_FROM}&To={{to}}&Body={{message}}"

routes:
  - path: /api/sms
    method: POST
    type: fetch
    inputs:
      - { name: to,      source: body, type: string, required: true, pattern: "^\\+[1-9]\\d{6,14}$" }
      - { name: message, source: body, type: string, required: true, min: 1, max: 1600 }
    fetch:
      request: twilio_send
      response:
        output_template: '{"sent": true, "sid": "{{.Body.sid}}"}'
```

The `Basic ${env:TWILIO_BASIC_AUTH}` value is the base64 of
`SID:TOKEN`. Compute it once:

```sh
export TWILIO_BASIC_AUTH=$(printf "$TWILIO_ACCOUNT_SID:$TWILIO_AUTH_TOKEN" | base64)
```

## Try it

```sh
export TWILIO_ACCOUNT_SID=AC...
export TWILIO_AUTH_TOKEN=...
export TWILIO_FROM=+14155551212
export TWILIO_BASIC_AUTH=$(printf "$TWILIO_ACCOUNT_SID:$TWILIO_AUTH_TOKEN" | base64)

wave serve server.yaml --port 8080

curl -X POST http://localhost:8080/api/sms \
  -H 'Content-Type: application/json' \
  -d '{
    "to":      "+14155551234",
    "message": "Your code is 654321"
  }'
# {"sent": true, "sid": "SM..."}
```

## Common pattern: SMS-based 2FA

```yaml
storage:
  app:
    tables:
      verify_codes:
        columns:
          - id         INTEGER PRIMARY KEY AUTOINCREMENT
          - phone      TEXT NOT NULL
          - code       TEXT NOT NULL
          - expires_at TEXT NOT NULL
          - used       INTEGER NOT NULL DEFAULT 0

routes:
  # POST /api/verify/request {phone}
  - path: /api/verify/request
    method: POST
    type: storage-access
    inputs:
      - { name: phone, source: body, type: string, required: true, pattern: "^\\+[1-9]\\d{6,14}$" }
    limits: [rate_3_per_min]                       # don't let attackers enumerate
    storage-access:
      source: app
      execute: |
        INSERT INTO verify_codes(phone, code, expires_at)
        VALUES (
          {{phone}},
          printf('%06d', abs(random()) % 1000000),
          datetime('now', '+10 minutes')
        )
        RETURNING code
      output_template: '{"sent": true}'
      then:                                         # follow-up after the INSERT
        - type: api
          ref: twilio_send
          inputs:
            to:      phone
            message: "Your code: {{.Data.code}}"

  # POST /api/verify/check {phone, code}
  - path: /api/verify/check
    method: POST
    type: storage-access
    inputs:
      - { name: phone, source: body, type: string, required: true }
      - { name: code,  source: body, type: string, required: true, pattern: "^[0-9]{6}$" }
    limits: [rate_5_per_min]
    storage-access:
      source: app
      execute: |
        UPDATE verify_codes
        SET used = 1
        WHERE phone = {{phone}}
          AND code  = {{code}}
          AND used  = 0
          AND expires_at > datetime('now');
        SELECT changes() AS valid
      output_template: '{"valid": {{.Data.valid}}}'
```

::: tip Magic-link OR SMS
Want **both** email magic-link AND SMS as login options? Add a
`type: match` route over the request body shape and route to the
right backend. See [device-detection recipe](/cookbook/device-detection)
for the pattern.
:::

## Production checklist

- [ ] **Rate-limit aggressively** on verify-request — SMS costs
      money and attackers will enumerate phone numbers. 3/min/IP
      is reasonable for a code-request endpoint.
- [ ] **Verify the source phone format** with the `pattern:` regex.
      The E.164 pattern shown above (`^\\+[1-9]\\d{6,14}$`) catches
      most invalid inputs before they hit Twilio.
- [ ] **Use Twilio Verify API** for production 2FA — it handles the
      code generation, expiry, and rate limiting for you (one HTTPS
      call per check). The same `type: fetch` shape works against
      `verify.twilio.com/v2`.
- [ ] **Outbox for marketing/digest SMS** — see [outbox recipe](/cookbook/outbox).
- [ ] **Don't log full phone numbers** in audit (PII). Hash or
      truncate.
- [ ] **International compliance** — different countries have
      different opt-in / quiet-hours rules. Twilio docs cover them.

## Other providers

Same `type: fetch` shape, different request def:

| Provider | URL | Auth header | Body |
|---|---|---|---|
| **Twilio** | `…/Messages.json` | Basic SID:TOKEN | form-encoded `From`, `To`, `Body` |
| **Vonage** | `https://rest.nexmo.com/sms/json` | none (api_key in body) | form `api_key`, `api_secret`, `from`, `to`, `text` |
| **MessageBird** | `https://rest.messagebird.com/messages` | `AccessKey API_KEY` | JSON `originator`, `recipients`, `body` |
| **AWS SNS** | `https://sns.us-east-1.amazonaws.com/` | Signature v4 | form `Action=Publish`, `Message`, `PhoneNumber` |

## See also

- [Send transactional email](/cookbook/send-email) — same pattern over email
- [Rate-limit an endpoint](/cookbook/rate-limit) — the verify-request rate cap
- [Outbox-backed delivery](/cookbook/outbox) — durable SMS queue for digests
- Built-in TOTP 2FA: [`examples/apps/totp-2fa`](https://github.com/luowensheng/wave/tree/main/examples/apps/totp-2fa) — no Twilio required (authenticator app instead)
