# Routes

The route is Wave's primary unit. One entry in `routes:` maps an HTTP
request to a handler.

## Anatomy

```yaml
- path: /items/{id}                  # mux pattern (Go 1.22+ ServeMux syntax)
  method: GET                        # or methods: [GET, HEAD] for multiple
  type: storage-access               # which handler to use
  auth: [primary]                    # require a valid session
  inputs:                            # declared, validated, typed
    - { name: id, source: path, type: int, required: true }
  storage-access:                    # type-specific config
    source: app
    execute: "SELECT * FROM items WHERE id = {{id}} LIMIT 1"
    if_empty_status: 404
    output_template: '{{toJSON .Data}}'
  cors_origins: ["*"]                # cross-cutting concerns
  cache: { ttl: 30s }
  limits: [rate_100rpm]
```

## The 28 route types

| Category | Type | Use for |
|---|---|---|
| **Data** | `storage-access` | Run SQL, return results |
| | `api` | Server-orchestrated upstream HTTP |
| | `fetch` | Server-initiated HTTP, simpler than `api` |
| | `content` | Hand-written static response |
| **Files** | `file` | Serve one file |
| | `static` | Serve a directory tree |
| | `file-server` | Directory listing + serve |
| **Proxy** | `forward` | Reverse-proxy to a backend |
| | `dynamic-forward` | Proxy with target from request |
| **Routing** | `match` | Predicate-based dispatch |
| | `redirect` | 302/307 to another URL |
| **Realtime** | `stream-publish` | Publish to an SSE broker |
| | `task` | Async work with SSE progress |
| **Auth** | `auth-login`, `auth-signup`, `auth-logout` | Form-based auth |
| | `magic-link-request`, `magic-link-consume` | Email magic link |
| | `totp-enroll-start`, `totp-enroll-confirm`, `totp-verify` | TOTP 2FA |
| | `oauth-start`, `oauth-callback` | OAuth 2.0 / OIDC |
| **Other** | `plugin` | Hand off to a plugin |
| | `graphql` | GraphQL endpoint |
| | `process` | Run a shell script per request |
| | `dependencies` | Resource health probe |

Each type has a corresponding `<name>:` config block. The full YAML
shape per type is in [CLAUDE.md](https://github.com/luowensheng/wave/blob/main/CLAUDE.md).

## Cross-cutting concerns

Any route can add these — they layer in the same middleware chain:

| Field | What it does |
|---|---|
| `auth: [name]` | Require a valid session from the named auth config |
| `require_roles: [admin]` | RBAC by claim |
| `require_claims: {...}` | RBAC by arbitrary claim key/value |
| `inputs: [...]` | Parse + validate request values |
| `validate_csrf: true` | CSRF token check on the form |
| `include_csrf: true` | Issue a CSRF token in a response cookie |
| `cors_origins: [...]` | Per-route CORS allowlist |
| `webhook_sig: { provider, secret }` | Verify webhook signature |
| `forward_auth: { url }` | Delegate auth to an external service |
| `ip_whitelist: [...]` / `ip_blacklist: [...]` | IP filtering |
| `cache: { ttl }` | Per-route response cache (GET only) |
| `limits: [name]` | Reference rate-limit / body-size / circuit-breaker entries |
| `request_schema: { inline }` | JSON Schema validation on the body |
| `expected_content_type` | Body parser hint (multipart / x-www-form / text) |

All of these stack in a deterministic order; see
[`servers.go wrapRouteMiddleware`](https://github.com/luowensheng/wave/blob/main/orchestrator/server/servers.go).

## Path-pattern syntax

Wave uses Go 1.22+'s `http.ServeMux` patterns:

- `/items` — exact match
- `/items/` — subtree match (anything starting with `/items/`)
- `/items/{id}` — path variable (`{{id}}` in inputs reads it)
- `/items/{id...}` — wildcard suffix

Combined with the optional `method:` prefix:

- `method: GET, path: /items/{id}` → mux pattern `"GET /items/{id}"`
- `methods: [POST, PUT], path: /items/{id}` → method-less mux
  pattern, allowedMethods enforced inside

::: warning method: vs methods:
Use `methods:` (plural) when you set `cors_origins:`. `method:`
(singular) bakes the verb into the mux pattern, which 405s OPTIONS
preflights before they reach the CORS wrapper. See the
[CORS recipe](/cookbook/cors-preflight) for the full footgun.
:::

## Route IDs and library-only routes

A route with `id:` is referenceable from `type: match` cases:

```yaml
- id: mobile_home
  path: /m/home
  type: content
  content: { ... }

- path: /
  type: match
  match:
    cases:
      - when: header
        match: { user-agent: { regex: "Mobile" } }
        route: mobile_home        # by-id reference
```

A route with `id:` but no `path:` is **library-only** — never
registered as an endpoint, only reachable via `route: <id>` from a
match case. Useful for keeping match YAML compact.

## The catch-all route

```yaml
default_route:
  type: content
  content:
    status_code: 404
    body: '{"error":"not found"}'
```

Mounted at `/` (the universal subtree in Go's ServeMux) so it
matches any path no other route claims. If `default_route` is
unset, Wave emits its own JSON 404 envelope:
`{"error":"page not found","path":"/foo"}`.

## See also

- [Cookbook](/cookbook/) — recipes covering every route type
- [Inputs](/guide/concepts-inputs)
- [Match routes](/cookbook/multi-tenant), [device detection](/cookbook/device-detection),
  [A/B testing](/cookbook/ab-testing) — predicate dispatch recipes
