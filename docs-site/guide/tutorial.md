# Tutorial: build a todo API end-to-end

A 30-minute walkthrough. By the end you'll have a real, deployable
todo API with auth, validation, audit logging, and an admin view.

The full final `server.yaml` is at the bottom of the page. Each
step is a small additive change you can paste in.

---

## What you'll build

```
POST   /signup          ?email=ada@example.com → magic link by email
GET    /auth/consume    ?token=...           → sets session cookie
POST   /todos           {text:"…"}           → create my todo
GET    /todos                                 → list my todos
PATCH  /todos/{id}      {done:true|false}    → toggle
DELETE /todos/{id}                            → delete
GET    /admin/all                             → admin-only: every user's todos
GET    /healthz                               → built-in probe
GET    /metrics                               → built-in Prometheus
```

Auth: magic-link email. Storage: SQLite. RBAC: admin role gates
`/admin/*`.

## Step 0 — install Wave

```sh
go install github.com/luowensheng/wave/orchestrator@latest
ln -s "$HOME/go/bin/orchestrator" /usr/local/bin/wave   # convenience
wave version
```

Create a project directory:

```sh
mkdir wave-todo && cd wave-todo
```

## Step 1 — Hello, world

`server.yaml`:

```yaml
default:
  port: 8080

routes:
  - path: /healthz-test
    method: GET
    type: content
    content:
      status_code: 200
      headers: [["Content-Type", "text/plain"]]
      body: "ok"
```

```sh
wave serve server.yaml --port 8080
curl http://localhost:8080/healthz-test
# ok
curl http://localhost:8080/healthz
# ok (built-in)
```

Kill the server (Ctrl-C). Now we're going to actually build something.

## Step 2 — Add storage

```yaml
default:
  port: 8080

storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      users:
        columns:
          - id    INTEGER PRIMARY KEY AUTOINCREMENT
          - email TEXT NOT NULL UNIQUE
          - role  TEXT NOT NULL DEFAULT 'user'
      todos:
        columns:
          - id      INTEGER PRIMARY KEY AUTOINCREMENT
          - user_id INTEGER NOT NULL
          - text    TEXT NOT NULL
          - done    INTEGER NOT NULL DEFAULT 0
          - at      TEXT NOT NULL DEFAULT (datetime('now'))

routes:
  - path: /healthz-test
    method: GET
    type: content
    content: { status_code: 200, body: "ok" }
```

Boot it; the `CREATE TABLE` statements run automatically. Check:

```sh
sqlite3 data.db ".schema"
```

## Step 3 — Add CRUD on todos (no auth yet)

```yaml
routes:
  - path: /todos
    method: POST
    type: storage-access
    inputs:
      - { name: text, source: body, type: string, required: true, min: 1, max: 1000 }
    storage-access:
      source: app
      execute: "INSERT INTO todos(user_id, text) VALUES (1, {{text}})"
      output_template: '{"id": {{.LastInsertID}}}'

  - path: /todos
    method: GET
    type: storage-access
    storage-access:
      source: app
      execute: "SELECT id, text, done FROM todos ORDER BY id DESC"
      output_template: '{{toJSON .Data}}'

  - path: /todos/{id}
    method: PATCH
    type: storage-access
    inputs:
      - { name: id,   source: path, type: int,  required: true }
      - { name: done, source: body, type: bool, required: true }
    storage-access:
      source: app
      execute: "UPDATE todos SET done = {{done}} WHERE id = {{id}}"
      output_template: '{"updated": {{.RowsAffected}}}'

  - path: /todos/{id}
    method: DELETE
    type: storage-access
    inputs:
      - { name: id, source: path, type: int, required: true }
    storage-access:
      source: app
      execute: "DELETE FROM todos WHERE id = {{id}}"
      output_template: '{"deleted": {{.RowsAffected}}}'
```

Try it:

```sh
curl -X POST -d '{"text":"buy milk"}' http://localhost:8080/todos
# {"id": 1}

curl http://localhost:8080/todos
# [{"done":0,"id":1,"text":"buy milk"}]

curl -X PATCH -d '{"done":true}' http://localhost:8080/todos/1
# {"updated": 1}
```

`user_id: 1` is hardcoded for now — we'll wire real auth next.

## Step 4 — Add magic-link auth

Add an `auth:` block and two new routes:

```yaml
env:
  JWT_SECRET: { description: "HMAC secret for session JWT" }

auth:
  app:
    type: jwt
    secret: "${env:JWT_SECRET}"
    cookie_name: session
    cookie_max_age_seconds: 86400        # 24h

# (storage: block unchanged)

routes:
  # Request a magic link
  - path: /signup
    method: POST
    type: magic-link-request
    inputs:
      - { name: email, source: body, type: email, required: true }
    magic-link-request:
      for: app
      email_field: email
      callback_path: /auth/consume
      email_subject: "Your todo app sign-in link"
      email_template: |
        Click to sign in: {{.link}}

  # Consume the magic link
  - path: /auth/consume
    method: GET
    type: magic-link-consume
    magic-link-consume:
      for: app
      redirect_on_success: /me
      redirect_on_failure: /

  # Protected: confirm who I am
  - path: /me
    method: GET
    auth: [app]
    type: storage-access
    storage-access:
      source: app
      execute: "SELECT id, email, role FROM users WHERE id = {{getUser}} LIMIT 1"
      output_template: '{{toJSON .Data}}'

  # ... existing /todos routes, but require auth and scope to current user (Step 5)
```

Run with a JWT secret:

```sh
JWT_SECRET=$(openssl rand -hex 32) wave serve server.yaml --port 8080

curl -X POST -d '{"email":"ada@example.com"}' http://localhost:8080/signup
# (dev mode logs the magic link to stdout)
```

Click the logged link in a browser. You land on `/me` with a session
cookie set.

## Step 5 — Scope todos to the current user

Add `auth: [app]` to every `/todos` route and use `{{getUser}}`:

```yaml
- path: /todos
  method: POST
  auth: [app]
  type: storage-access
  inputs:
    - { name: text, source: body, type: string, required: true, min: 1, max: 1000 }
  storage-access:
    source: app
    execute: "INSERT INTO todos(user_id, text) VALUES ({{getUser}}, {{text}})"
    output_template: '{"id": {{.LastInsertID}}}'

- path: /todos
  method: GET
  auth: [app]
  type: storage-access
  storage-access:
    source: app
    execute: "SELECT id, text, done FROM todos WHERE user_id = {{getUser}} ORDER BY id DESC"
    output_template: '{{toJSON .Data}}'

- path: /todos/{id}
  method: PATCH
  auth: [app]
  type: storage-access
  inputs:
    - { name: id,   source: path, type: int,  required: true }
    - { name: done, source: body, type: bool, required: true }
  storage-access:
    source: app
    # WHERE user_id = {{getUser}} prevents users editing each others' todos
    execute: "UPDATE todos SET done = {{done}} WHERE id = {{id}} AND user_id = {{getUser}}"
    output_template: '{"updated": {{.RowsAffected}}}'

- path: /todos/{id}
  method: DELETE
  auth: [app]
  type: storage-access
  inputs:
    - { name: id, source: path, type: int, required: true }
  storage-access:
    source: app
    execute: "DELETE FROM todos WHERE id = {{id}} AND user_id = {{getUser}}"
    output_template: '{"deleted": {{.RowsAffected}}}'
```

`{{getUser}}` reads the authenticated user ID from the JWT
claims — no chance of forgetting to scope a query.

## Step 6 — Admin-only route

```yaml
- path: /admin/all
  method: GET
  auth: [app]
  require_roles: [admin]
  type: storage-access
  storage-access:
    source: app
    execute: |
      SELECT u.email, t.id, t.text, t.done, t.at
      FROM todos t JOIN users u ON u.id = t.user_id
      ORDER BY t.id DESC LIMIT 200
    output_template: '{{toJSON .Data}}'
```

Promote a user to admin:

```sh
sqlite3 data.db "UPDATE users SET role = 'admin' WHERE email = 'ada@example.com'"
```

The JWT issued by the magic-link backend includes the `role` claim
on next login. Visit `/admin/all` — you see everyone's todos.

A non-admin user hitting `/admin/all` gets 403.

## Step 7 — Audit log every mutation

```yaml
storage:
  app:
    # ... existing tables ...
    tables:
      # ... users, todos ...
      audit_log:
        columns:
          - id     INTEGER PRIMARY KEY AUTOINCREMENT
          - actor  TEXT NOT NULL
          - action TEXT NOT NULL
          - target TEXT NOT NULL
          - before TEXT
          - after  TEXT
          - ip     TEXT
          - at     TEXT NOT NULL DEFAULT (datetime('now'))
```

Then expand the PATCH route to log before/after atomically:

```yaml
- path: /todos/{id}
  method: PATCH
  auth: [app]
  type: storage-access
  inputs:
    - { name: id,   source: path, type: int,  required: true }
    - { name: done, source: body, type: bool, required: true }
  storage-access:
    source: app
    execute: |
      INSERT INTO audit_log(actor, action, target, before, after, ip)
        SELECT {{getUser}}, 'todo.update', 'todo:' || id,
               json_object('done', done),
               json_object('done', {{done}}),
               {{getClientIP}}
        FROM todos WHERE id = {{id}} AND user_id = {{getUser}};
      UPDATE todos SET done = {{done}} WHERE id = {{id}} AND user_id = {{getUser}};
      SELECT id, text, done FROM todos WHERE id = {{id}} LIMIT 1
    if_empty_status: 404
    output_template: '{{toJSON .Data}}'
```

The audit row and the update run as one transaction — if the update
fails, the audit row is rolled back too.

## Step 8 — Rate-limit signup

Add `limits:` and reference it:

```yaml
limits:
  signup_5_per_min:
    case: rate_limited
    rps: 0.0833    # 5 per 60s
    burst: 3
    on_fail:
      status_code: 429
      body: '{"error":"too many signup attempts, try again later"}'

routes:
  - path: /signup
    method: POST
    type: magic-link-request
    limits: [signup_5_per_min]
    # ... rest unchanged ...
```

Now signup is bucketed per IP, capped at 5/minute with a burst of
3.

## Step 9 — Deploy

```sh
# Test the config without serving
wave validate server.yaml
# ok

# Run live diagnostics
wave doctor server.yaml --json | jq

# Build a Docker image
docker build -t my-todo:0.1 .

# Or ship to Fly.io
fly launch
```

See [Docker deploy](/guide/deploy-docker) or
[Fly deploy](/guide/deploy-fly) for the full setup.

## What you skipped that you'd add for production

- **CSRF** on the form-style routes (`validate_csrf: true`)
- **Body-size limit** via `limits[body_too_large]`
- **`cookie_secure: true`** in `auth.app` (requires HTTPS)
- **Prometheus scraping** of `/metrics` and dashboards
- **Backup** via Litestream or volume snapshots
- **OAuth as an alternative login method** — see [OAuth recipe](/cookbook/oauth)
- **2FA** via TOTP — see [`totp-2fa`](https://github.com/luowensheng/wave/tree/main/examples/apps/totp-2fa)

See the [Production checklist](/guide/deploy-checklist) for the
exhaustive list.

## The complete server.yaml

::: details Click to expand
```yaml
default:
  port: 8080

env:
  JWT_SECRET: { description: "HMAC secret for session JWT" }

auth:
  app:
    type: jwt
    secret: "${env:JWT_SECRET}"
    cookie_name: session
    cookie_max_age_seconds: 86400

limits:
  signup_5_per_min:
    case: rate_limited
    rps: 0.0833
    burst: 3
    on_fail:
      status_code: 429
      headers: [["Content-Type", "application/json"]]
      body: '{"error":"too many signup attempts, try again later"}'

storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      users:
        columns:
          - id    INTEGER PRIMARY KEY AUTOINCREMENT
          - email TEXT NOT NULL UNIQUE
          - role  TEXT NOT NULL DEFAULT 'user'
      todos:
        columns:
          - id      INTEGER PRIMARY KEY AUTOINCREMENT
          - user_id INTEGER NOT NULL
          - text    TEXT NOT NULL
          - done    INTEGER NOT NULL DEFAULT 0
          - at      TEXT NOT NULL DEFAULT (datetime('now'))
      audit_log:
        columns:
          - id     INTEGER PRIMARY KEY AUTOINCREMENT
          - actor  TEXT NOT NULL
          - action TEXT NOT NULL
          - target TEXT NOT NULL
          - before TEXT
          - after  TEXT
          - ip     TEXT
          - at     TEXT NOT NULL DEFAULT (datetime('now'))

routes:
  - path: /signup
    method: POST
    type: magic-link-request
    limits: [signup_5_per_min]
    inputs: [{ name: email, source: body, type: email, required: true }]
    magic-link-request:
      for: app
      email_field: email
      callback_path: /auth/consume
      email_template: "Sign in: {{.link}}"

  - path: /auth/consume
    method: GET
    type: magic-link-consume
    magic-link-consume:
      for: app
      redirect_on_success: /me
      redirect_on_failure: /

  - path: /me
    method: GET
    auth: [app]
    type: storage-access
    storage-access:
      source: app
      execute: "SELECT id, email, role FROM users WHERE id = {{getUser}} LIMIT 1"
      output_template: '{{toJSON .Data}}'

  - path: /todos
    method: POST
    auth: [app]
    type: storage-access
    inputs: [{ name: text, source: body, type: string, required: true, min: 1, max: 1000 }]
    storage-access:
      source: app
      execute: "INSERT INTO todos(user_id, text) VALUES ({{getUser}}, {{text}})"
      output_template: '{"id": {{.LastInsertID}}}'

  - path: /todos
    method: GET
    auth: [app]
    type: storage-access
    storage-access:
      source: app
      execute: "SELECT id, text, done FROM todos WHERE user_id = {{getUser}} ORDER BY id DESC"
      output_template: '{{toJSON .Data}}'

  - path: /todos/{id}
    method: PATCH
    auth: [app]
    type: storage-access
    inputs:
      - { name: id,   source: path, type: int,  required: true }
      - { name: done, source: body, type: bool, required: true }
    storage-access:
      source: app
      execute: |
        INSERT INTO audit_log(actor, action, target, before, after, ip)
          SELECT {{getUser}}, 'todo.update', 'todo:' || id,
                 json_object('done', done),
                 json_object('done', {{done}}),
                 {{getClientIP}}
          FROM todos WHERE id = {{id}} AND user_id = {{getUser}};
        UPDATE todos SET done = {{done}} WHERE id = {{id}} AND user_id = {{getUser}};
        SELECT id, text, done FROM todos WHERE id = {{id}} LIMIT 1
      if_empty_status: 404
      output_template: '{{toJSON .Data}}'

  - path: /todos/{id}
    method: DELETE
    auth: [app]
    type: storage-access
    inputs: [{ name: id, source: path, type: int, required: true }]
    storage-access:
      source: app
      execute: "DELETE FROM todos WHERE id = {{id}} AND user_id = {{getUser}}"
      output_template: '{"deleted": {{.RowsAffected}}}'

  - path: /admin/all
    method: GET
    auth: [app]
    require_roles: [admin]
    type: storage-access
    storage-access:
      source: app
      execute: |
        SELECT u.email, t.id, t.text, t.done, t.at
        FROM todos t JOIN users u ON u.id = t.user_id
        ORDER BY t.id DESC LIMIT 200
      output_template: '{{toJSON .Data}}'
```
:::

## Next steps

- [Cookbook](/cookbook/) — copy-paste recipes for common patterns
- [Concepts](/guide/concepts-routes) — deeper dives on each subsystem
- [Production checklist](/guide/deploy-checklist) — before you ship
- [AI agents](/ai/) — work with Wave using LLMs
