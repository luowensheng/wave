---
name: wave
description: |
  Authoring help for Wave server.yaml configs. Use whenever the user
  is working with Wave (a declarative HTTP server framework), is
  editing a server.yaml, or asks about route types, storage,
  inputs, plugins, or scheduling in a Wave project.

  Triggers: user mentions "wave", "server.yaml", "type: storage-access",
  "type: match", "type: task", "type: schedule", "wave serve", "wave doctor".
  Skip: user is working on an unrelated Go project that happens to
  have a yaml file.
---

# Wave skill

Wave is a YAML-driven Go HTTP framework. Routes, storage, plugins,
and schedules are declared in a single `server.yaml`. Wave compiles
that into a working server with middleware (auth, CSRF, rate limit,
CORS, audit) and a single binary.

The canonical reference is **[CLAUDE.md](../../CLAUDE.md)** in the
repo root. Read it before making non-trivial edits.

---

## The four non-negotiable rules

Every Wave config follows these. Violations cause silent runtime
failures.

### 1. SQL is parameterised via `{{name}}`

Inside `execute:` strings, `{{name}}` becomes a `?` placeholder
bound to the value of the declared input named `name`.

```yaml
# ✅ Good
execute: "INSERT INTO users(name) VALUES ({{name}})"

# ❌ NEVER. dot-notation interpolates the literal — SQL injection.
execute: "INSERT INTO users(name) VALUES ({{.name}})"

# ❌ NEVER. bind with dot-navigation bypasses parameterisation.
execute: "INSERT INTO users(name) VALUES ({{bind .user.name}})"

# ❌ NEVER. toJSON inside SQL injects raw JSON.
execute: "INSERT INTO data(blob) VALUES ({{toJSON .body}})"
```

### 2. Every `{{name}}` in SQL must be a declared input

```yaml
inputs:
  - { name: user_id, source: path,  type: int,    required: true }
  - { name: email,   source: body,  type: email,  required: true }
storage-access:
  execute: "INSERT INTO users(id, email) VALUES ({{user_id}}, {{email}})"
```

If `execute:` references `{{undeclared}}`, the route returns 500
("undeclared input"). Catch this in unit tests.

### 3. Method dispatch + CORS preflight

When `cors_origins:` is set, use `methods:` (plural) — not `method:`
(singular):

```yaml
# ❌ method:post pattern is registered as "POST /path" in the mux,
#    so OPTIONS preflights 405 before reaching the CORS wrapper.
- path: /api/x
  method: post
  cors_origins: ["*"]

# ✅ methods:[POST] leaves the pattern method-less; OPTIONS passes
#    through to the CORS wrapper which short-circuits with 204.
- path: /api/x
  methods: [POST]
  cors_origins: ["*"]
```

### 4. Single-row queries use `LIMIT 1`

Wave detects `LIMIT 1` and exposes `.Data.column` (map access).
Without it, `.Data` is a slice and you must use `{{toJSON .Data}}`.

```yaml
# ✅ Single-row — access fields directly
execute: "SELECT id, name FROM users WHERE id = {{id}} LIMIT 1"
output_template: '{"id": {{.Data.id}}, "name": "{{.Data.name}}"}'

# ✅ Multi-row — JSON-encode the slice
execute: "SELECT id, name FROM users ORDER BY id"
output_template: '{{toJSON .Data}}'
```

---

## Route type quick reference

Pick the right route type for the job. Each one's full spec is in
[CLAUDE.md](../../CLAUDE.md).

| You want to… | Use |
|---|---|
| Run a SQL query and return the result | `type: storage-access` |
| Multi-step query pipeline (auth then fetch) | `type: storage-access` with `steps:` |
| Run an upstream API call from inside Wave | `type: api` or `type: fetch` |
| Reverse-proxy to a backend | `type: forward` |
| Reverse-proxy with target from request | `type: dynamic_forward` |
| Long-running task with progress events | `type: task` (with `connection: events`) |
| Cron / scheduled job | top-level `schedule:` (not a route) |
| Dispatch by method/header/cookie/host | `type: match` |
| Serve static files / directory | `type: file-server` or `type: static` |
| One-off file | `type: file` |
| Hand-written response | `type: content` |
| Email login | `type: magic-link-request` + `magic-link-consume` |
| TOTP 2FA | `type: totp-enroll-start/confirm/verify` |
| OAuth | `type: oauth-start` + `oauth-callback` |
| Login/signup/logout forms | `type: auth-login/auth-signup/auth-logout` |
| 302 redirect | `type: redirect` |
| Shell process | `type: process` |
| GraphQL endpoint | `type: graphql` |
| Server-Sent Events publish | `type: stream-publish` |
| Server-side composition (multiple files) | top-level `include:` |

---

## Adding a NEW route type

Five-step checklist, in order. Skipping a step causes silent misses
at boot.

1. **Create `usecases/<name>/config.go`** with a `Config` struct
   (YAML tags!) and a method:
   ```go
   func (c *Config) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error)
   ```
   Validate config at boot, not at request time.

2. **Create `usecases/routes/<name>_config.go`** with a type alias:
   ```go
   package routes
   import "github.com/luowensheng/wave/usecases/<name>"
   type <Name>Config = <name>.Config
   ```

3. **Add field on `Route`** in `orchestrator/server/route.go`:
   ```go
   <Name>Config *routes.<Name>Config `yaml:"<name>,omitempty" json:"<name>,omitempty"`
   ```

4. **Add case** in `getRouteConfig()` in the same file:
   ```go
   case "<name>":
       routeConfig = route.<Name>Config
   ```

5. **Write a test** in `usecases/<name>/config_test.go` following the
   pattern in `usecases/match/config_test.go`.

Optionally — if the route needs injected dependencies (storage,
plugins) — add a package-level `var XxxFn func(...)` and wire it in
`InitDependencies` in `orchestrator/server/servers.go`.

---

## Common YAML idioms

### Conditional SQL clauses

```yaml
execute: |
  SELECT * FROM items
  WHERE 1=1
  {{if hasvalue "q"}} AND name LIKE {{wrap "%q%"}} {{end}}
  ORDER BY id DESC
```

`hasvalue`, `hasvalues` (AND), `hasanyvalue` (OR) are the gating
helpers.

### JSON array inputs into `IN (...)` clauses

```yaml
inputs:
  - { name: ids, source: body, type: array, required: true }
storage-access:
  execute: >
    SELECT * FROM items
    WHERE id IN (SELECT value FROM json_each({{jsonArray (raw "ids")}}))
```

### Multi-statement (UPDATE then SELECT)

```yaml
execute: |
  UPDATE items SET views = views + 1 WHERE slug = {{slug}};
  SELECT slug, content, views FROM items WHERE slug = {{slug}} LIMIT 1
```

The last `SELECT` drives the response.

### Predicate routing (device, A/B, tenant)

```yaml
- path: /
  type: match
  match:
    cases:
      - when: header
        match:
          user-agent: { regex: "Mobile|iPhone|Android" }
        route: { type: forward, forward: { forward_url: "http://mobile/" } }
      - when: cookie
        match: { variant: beta }
        route: my_beta_handler        # id reference to another route
    default:
      route: my_default_handler
```

A route with `id:` but no `path:` is library-only — never registered
as an endpoint, only reachable from `match` cases.

---

## Things that look right but aren't

- ❌ `method: post`, `method: get` — works for non-CORS routes only.
  For CORS preflight to work, use `methods: [POST]` etc.
- ❌ `{{.Data}}` in SQL — output-template syntax leaking into SQL.
- ❌ Top-level `default:` named the same as the catch-all `default_route:`
  — different things. `default:` sets host/port; `default_route:` is
  the catch-all 404 handler.
- ❌ A route with no `path` and no `id` — boot error. Library-only
  routes need an `id`.

---

## When you're stuck

Read the relevant section of [CLAUDE.md](../../CLAUDE.md). It is the
canonical guide and covers every edge case the framework currently
handles.

Look at [examples/apps/](../../examples/apps/) — every common
pattern has a demo. `match-route-demo`, `pipeline-demo`,
`background-task-demo`, `outbox-reliability-demo`, `magic-link-login`
are the most copied.
