# Magic-link login

Passwordless email auth. User enters their email, gets a one-shot
link, clicks it, lands logged in.

## How the flow works

```
1. POST /login        { email }
   → Wave inserts a one-shot token, sends an email
2. GET /auth/consume?token=…
   → Wave validates the token, sets a session cookie, redirects
```

Two routes do the work. Wave handles token storage, email delivery,
expiry, and cookie-setting.

## YAML

```yaml
default:
  port: 8080

auth:
  app:
    type: jwt
    secret: "${env:JWT_SECRET}"
    cookie_name: session
    cookie_max_age_seconds: 86400  # 24h

storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      users:
        columns:
          - id    INTEGER PRIMARY KEY AUTOINCREMENT
          - email TEXT NOT NULL UNIQUE

routes:
  # Request the magic link
  - path: /login
    method: POST
    type: magic-link-request
    inputs:
      - { name: email, source: body, type: email, required: true }
    magic-link-request:
      for: app
      email_field: email
      callback_path: /auth/consume
      email_subject: "Sign in to Wave demo"
      email_template: |
        Click to sign in: {{.link}}
        Expires in 15 minutes.

  # Consume the magic link
  - path: /auth/consume
    method: GET
    type: magic-link-consume
    magic-link-consume:
      for: app
      redirect_on_success: /me
      redirect_on_failure: /login?err=expired

  # Protected page — only reachable with a valid session cookie
  - path: /me
    method: GET
    type: content
    auth: [app]
    content:
      status_code: 200
      headers: [["Content-Type", "text/plain"]]
      body: "logged in"
```

## Try it

```sh
JWT_SECRET=demo-secret wave serve server.yaml --port 8080

# Request a link (the email is logged to stdout in dev mode)
curl -X POST -d '{"email":"ada@example.com"}' http://localhost:8080/login

# Copy the link from the dev log, then visit it in a browser.
# You land on /me with a session cookie set.
```

## Production checklist

- **Email backend**: set `auth_flows.smtp.host` (or wire a plugin)
  to send real email. Dev mode logs to stdout.
- **JWT_SECRET**: 32+ random bytes, kept in a secrets manager.
- **HTTPS**: set `cookie_secure: true` in the auth config.
- **Rate-limit `/login`** to prevent enumeration (see
  [Rate-limit an endpoint](/cookbook/rate-limit)).

## Variations

- **TOTP after magic-link** for high-value accounts:
  [`examples/apps/magic-link-plus-totp`](https://github.com/luowensheng/wave/tree/main/examples/apps/magic-link-plus-totp)
- **Pair with OAuth** (let users pick magic-link OR Google):
  [OAuth recipe](/cookbook/oauth)

## See also

- Demo: [`examples/apps/magic-link-login`](https://github.com/luowensheng/wave/tree/main/examples/apps/magic-link-login)
- Concepts: [Auth](/guide/concepts-auth)
