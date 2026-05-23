# Full feature inventory

Every capability Wave ships, in one browseable page. Use this when
you're trying to answer "does Wave have X?" fast. Each entry links
to the deeper docs that show you how to use it.

::: tip Skimming this page
- **Bold** entries are first-class features wired into the framework
- *Italic* entries are experimental, stubbed, or plugin-only
- Every YAML key is shown in `monospace` so you can ⌘F for it
:::

## Headline numbers

| What | Count |
|---|---:|
| Route types | **28** |
| Per-route middleware | **16** (+ 6 outer) |
| CLI commands | **14** top-level (+ 7 subcommands) |
| Auth types built-in | **7** |
| Webhook signature providers | **4** |
| Plugin kinds | **5** |
| Plugin transports | **2** (`process`, `http`) + *gRPC/WASM stubbed* |
| Connection types | **3** (`sse`, `ws`, `auto`) |
| Scheduler action types | **3** (`api`, `plugin`, `storage`) |
| Scheduler sink types | **5** (`storage`, `publish`, `plugin`, `api`, `for_each`) |
| Built-in HTTP endpoints | **6+** |
| Scaffold templates (`wave init`) | **7** |
| Demo apps (`examples/apps/`) | **64** |
| Runnable test suites (`*.test.yaml`) | **3** |

---

## All 28 route types

Every `type:` you can put on a route. The full YAML shape for each
is in [CLAUDE.md](https://github.com/luowensheng/wave/blob/main/CLAUDE.md).

| `type:` | What it does | YAML config block |
|---|---|---|
| `storage-access` | Run SQL, return results (single-shot or multi-step pipeline) | `storage-access:` |
| `api` | Execute a named outbound HTTP request from `requests:` registry | `api:` |
| `fetch` | Declarative outbound HTTP (inputs as template data) | `fetch:` |
| `content` | Serve literal text / HTML / JSON content | `content:` |
| `forward` | HTTP reverse-proxy to an upstream | `forward:` |
| `dynamic-forward` | Proxy with target URL selected from request | `dynamic-forward:` |
| `match` | Predicate-based per-request router (host / header / cookie / query / method / IP / path var) | `match:` |
| `redirect` | 301 / 302 / 307 / 308 with templated URL | `redirect:` |
| `static` | Serve a directory tree | `static:` |
| `file` | Serve a single file | `file:` |
| `file-server` | Directory listing + serve with caching headers | `file-server:` |
| `stream-publish` | Publish events to a named SSE/WS broker (with `webhook_sig:` it's also a receiver) | `stream-publish:` |
| `task` | Trigger a scheduled job on demand | `task:` |
| `plugin` | JSON-RPC call to a subprocess / HTTP / longlived plugin | `plugin:` |
| `process` | Execute a shell script per request, stream stdout | `process:` |
| `graphql` | GraphQL query engine over a storage backend | `graphql:` |
| `dependencies` | Expose installed dependencies as JSON | `dependencies:` |
| `auth-login` | Built-in login (JWT/session issuance) | `auth-login:` |
| `auth-signup` | Built-in user registration | `auth-signup:` |
| `auth-logout` | Logout (session/cookie revocation) | `auth-logout:` |
| `magic-link-request` | Email a passwordless login link | `magic-link-request:` |
| `magic-link-consume` | Validate a magic-link token, issue session | `magic-link-consume:` |
| `oauth-start` | Begin OAuth 2.0 / OIDC authorization-code flow | `oauth-start:` |
| `oauth-callback` | Exchange OAuth code for session | `oauth-callback:` |
| `totp-enroll-start` | Generate TOTP secret, return QR | `totp-enroll-start:` |
| `totp-enroll-confirm` | Confirm first TOTP code, save secret | `totp-enroll-confirm:` |
| `totp-verify` | Verify TOTP code during login | `totp-verify:` |

→ [Route concepts](/guide/concepts-routes) · [Cookbook (16 recipes)](/cookbook/)

---

## Per-route middleware

Every cross-cutting concern is **declarative**. Add a field to your
route; Wave wires the middleware. Applied innermost → outermost:

| Field | Triggers | What it does |
|---|---|---|
| `methods:` | always | 405 on disallowed verb |
| `validate_csrf:` | `true` | Verify CSRF token; reject if absent/wrong |
| `include_csrf:` | `true` | Inject CSRF token in response cookie |
| `require_roles:` | non-empty list | RBAC: AND'd against ID-token `roles`/`groups` claim |
| `require_claims:` | non-empty map | RBAC: AND'd exact-match against any claim |
| `auth:` | non-empty list | Require valid session from named auth config(s) |
| `ip_whitelist:` / `ip_blacklist:` | non-empty | Per-route IP filter (overrides global) |
| `request_schema:` | inline or path | JSON-Schema validation of body; 400 + error list |
| `inputs:` | non-empty | Parse + validate per-route inputs; 400 + error list |
| `forward_auth:` | non-empty | Delegate auth to external service (Authelia / Authentik / oauth2-proxy) |
| `webhook_sig:` | provider+secret | Verify HMAC signature; 401 on failure |
| `limits: [circuit_open]` | named entry | Circuit breaker: 503 when open; cooldown |
| `cache:` | non-empty | Per-route response cache (GET only, key-by-auth optional) |
| `limits: [rate_limited]` | named entry | Token-bucket rate limiter (key by IP or claim) |
| `limits: [body_too_large]` | named entry | Body size limit; 413 |
| `limits: [error]` | named entry | Swap status codes (e.g. 500 → custom HTML) |
| `cors_origins:` | non-empty | Per-route CORS; reflective OPTIONS preflight |

**Outer middleware** wraps the whole handler (request-id, security
headers, gzip, recovery, access logs) and is always on.

→ [Routes concept](/guide/concepts-routes) · [Rate-limit recipe](/cookbook/rate-limit) · [Audit log recipe](/cookbook/audit-log)

---

## Top-level config blocks

Every key you can put in `server.yaml`:

| Key | What |
|---|---|
| `default:` | Bind defaults (host, port, expected_content_type) |
| `env:` | Declared environment variables with descriptions |
| `args:` | Declared CLI args with descriptions |
| `storage:` | Named storage backends (SQLite or plugin) |
| `plugins:` | Named plugin configs (transport, command, kind) |
| `connections:` | Named SSE/WS brokers |
| `auth:` | Named auth providers (jwt, oidc, oauth2, magic_link, totp, plugin) |
| `auth_flows:` | Email/SMS senders for magic-link/TOTP |
| `schedule:` | Cron-like jobs (`every:` or `at:`) with action + sinks |
| `requests:` | Named outbound HTTP request defs (referenced by `type: api` / `fetch` / scheduler) |
| `routes:` | The route list |
| `default_route:` | Server-wide catch-all at `/` |
| `not_found:` | Custom 404 handler |
| `limits:` | Named limit-entry registry (rate_limited, body_too_large, circuit_open, etc.) |
| `ip_filter:` | Global IP whitelist/blacklist |
| `outbox_db:` | SQLite path for durable outbound webhook queue |
| `https_config:` | TLS settings (key/cert paths or generate self-signed) |
| `build:` | Frontend build pipeline (esbuild, watch mode) |
| `observability:` | Plugin-based exporter fanout config |
| `include:` | Composition references (other YAML files + optional path prefix) |
| `kind:` | Marks file as a typed library (not bootable) |
| `json_discovery_route_path:` / `html_discovery_route_path:` | Auto-register endpoints listing all routes |

→ [Wave in your stack](/guide/wave-in-your-stack) · [Concepts overview](/guide/concepts-routes)

---

## Input system

`inputs:` declares every value a route accepts. Wave parses, coerces,
validates, and exposes them safely to SQL templates.

**Sources:** `path`, `query`, `body`, `form`, `header`, `cookie`, `body_raw`

**Types:** `string`, `int`, `float`, `bool`, `email`, `uuid`, `file`, `bytes`, `array`, `object`

**Validators:** `required`, `min`, `max`, `pattern`, `enum`, `default`

**Body content types** (`expected_content_type:`): `application/json`
(default), `application/x-www-form-urlencoded`, `multipart/form-data`,
`text/plain`, `application/octet-stream`

→ [Inputs concept](/guide/concepts-inputs)

---

## Storage features

Declarative SQLite (and pluggable for everything else).

| Feature | How |
|---|---|
| Auto-create tables on boot | `storage.<name>.tables:` with column definitions |
| Multi-statement SQL | `execute:` with `;`-separated statements (run as one transaction) |
| Multi-step pipelines | `storage-access.steps:` with `as:` keys + dot-path inputs |
| Single-row vs multi-row auto-detect | Wave inspects `.Data` shape for template rendering |
| 404 when empty | `if_empty_status: 404` |
| Stream binary files | `response_content_type: $filetype` reads MIME from row |
| Per-step strict-scope inputs | `inputs: { name: "accum.path" }` — no implicit access |
| SQL injection impossible | Every `{{name}}` becomes a `?` parameter binding |

**SQL template helpers** (in `execute:`):

| Helper | Emits | Purpose |
|---|---|---|
| `{{name}}` | `?` | Bind a declared input |
| `{{getCurrentTime}}` | `?` | Server-side UTC timestamp |
| `{{wrap "value"}}` | `?` | Wrap with `%value%` for LIKE patterns |
| `{{hasvalue "key"}}` | bool | Guard for conditional clauses (`{{if hasvalue "q"}}…{{end}}`) |
| `{{jsonArray (raw "name")}}` | JSON literal | For `json_each` array-input queries |
| `{{getUser}}` | `?` | Authenticated user id from session |
| `{{getClientIP}}` | `?` | Best-guess source IP |
| `{{raw "name"}}` | Go value | Escape hatch — only inside `jsonArray` |

→ [Storage concept](/guide/concepts-storage) · [JSON API recipe](/cookbook/json-api)

---

## Auth providers

| Type | Use for | Notes |
|---|---|---|
| `jwt` | Internal apps with username/password | HS256, cookie or header |
| `oidc` | Google / Okta / Auth0 / Entra | Discovery + offline ID-token verify |
| `oauth2` | GitHub / Apple / custom | Authorization-code flow |
| `magic_link` | Passwordless email | SQLite or in-memory token store |
| `totp` | 2FA on top of any other auth | RFC 6238, QR code generation |
| `saml` | Enterprise SSO | Via `saml-auth` plugin (not built-in) |
| `plugin` | LDAP / proprietary IdP / custom | Plugin implements Authenticate / RefreshClaims / Logout |

**Cookie attributes** (per auth config): `cookie_name`, `cookie_secure`,
`cookie_same_site` (Strict/Lax/None), `cookie_domain`,
`cookie_max_age_seconds`.

**RBAC**: `require_roles: []` (read from `roles`/`groups` ID-token
claim) AND `require_claims: {key: value}` (exact-match any claim).

**Forward auth**: delegate to Authelia / Authentik / oauth2-proxy
via `forward_auth: { url, method, timeout_sec, forward_headers,
response_headers, trust_forwarded_for }`.

**CSRF**: `validate_csrf: true` + `include_csrf: true` (double-submit
or session-bound).

→ [Auth concept](/guide/concepts-auth) · [Magic-link recipe](/cookbook/magic-link-login) · [OAuth recipe](/cookbook/oauth)

---

## Webhook signature providers

All under `webhook_sig:` per-route. Verified before the handler runs;
401 on failure (overridable via `limits[missing_signature]`).

| `provider:` | Header | Algorithm | Replay protection |
|---|---|---|---|
| `stripe` | `Stripe-Signature` | HMAC-SHA256 + timestamp | ±5 min tolerance (configurable) |
| `github` | `X-Hub-Signature-256` | HMAC-SHA256 | none (none in the spec) |
| `slack` | `X-Slack-Signature` + `X-Slack-Request-Timestamp` | HMAC-SHA256 (`v0=...`) | ±5 min tolerance |
| `generic` | configurable (default `X-Signature`) | HMAC-SHA256 or SHA1; configurable `header_prefix` | none |

→ [Stripe webhooks recipe](/cookbook/stripe-webhooks)

---

## Observability

| Pillar | What ships | Where |
|---|---|---|
| Health | `/healthz`, `/readyz`, `/version` | Built-in, always on |
| Metrics | `/metrics` Prometheus exposition; per-route counters + histograms; outbox queue depth; SSE subscriber counts | `infra/metrics`, `infra/observability` |
| Traces | OpenTelemetry — every route, downstream HTTP, SQL queries | `infra/observability` |
| Logs | JSON structured per-request log (method/path/status/duration/request-id/IP/user-id) | `infra/logger` |
| Audit | Append-only event sink (action / actor / target / outcome / IP / ts) | `infra/audit` |
| Exporter fanout | Plugins with `kind: exporter` receive every metric/log/trace batch | `infra/observability/fanout` |
| OpenAPI | `/openapi.json` auto-generated from declared routes + inputs | optional |
| Route discovery | `json_discovery_route_path:` / `html_discovery_route_path:` | optional |
| Studio dashboard | `wave studio` web UI for cross-project ops | `wave studio` |

→ [Observability concept](/guide/concepts-observability)

---

## Reliability primitives

| Primitive | Where to configure | Behavior |
|---|---|---|
| Rate limit | `limits[rate_limited]` + per-route `limits:` | Token bucket; key by IP or claim |
| Body size limit | `limits[body_too_large]` | Reject > N bytes with 413 |
| Circuit breaker | `limits[circuit_open]` | Trip on N failures, cooldown for M seconds |
| Response cache | per-route `cache:` | GET-only, TTL, max-entries, key-by-auth optional |
| Request schema validation | per-route `request_schema:` | JSON-Schema; inline or `path:` to file |
| Outbox | top-level `outbox_db:` | Durable webhook queue + retries + DLQ + replay CLI |
| Graceful shutdown | always | SIGINT/SIGTERM → drain (10s max) |
| Plugin retries | per-plugin `retries:` + `retry_backoff:` | Exponential backoff 50ms-2s with jitter |

→ [Rate-limit recipe](/cookbook/rate-limit) · [Outbox recipe](/cookbook/outbox) · [Production checklist](/guide/deploy-checklist)

---

## Connections (SSE / WebSocket)

Declared under `connections:`. Each one auto-registers a `GET
<subscribe_path>` endpoint.

| Field | What |
|---|---|
| `type:` | `sse`, `ws` (currently degrades to SSE), `auto` (upgrade if browser supports) |
| `subscribe_path:` | Auto-registered route (e.g. `/events/chat`) |
| `subscribe_auth:` | Auth configs that guard subscription |
| `subscribe_cors_origins:` | CORS allowlist for subscribe |
| `buffer_size:` | Per-client event ring buffer (default 64) |
| `max_clients:` | Connection cap (default 256) |
| `keep_alive_interval:` | Heartbeat interval (default 15s) |

**Publishing**: `type: stream-publish` routes post `{event_type, data}`;
events fan out to all subscribed clients. `Last-Event-ID` header is
supported for reconnect-replay.

→ [SSE recipe](/cookbook/sse)

---

## Plugins

Out-of-process workers extending Wave. Same JSON contract used by
route handlers, storage backends, auth providers, secrets resolvers,
and observability exporters.

**Transports:**

| `kind:` field | What |
|---|---|
| (empty, transport=process) | Subprocess — one-shot per call |
| transport=`http` | Remote HTTP service |
| transport=`longlived` | Subprocess held open, framed JSON over stdin/stdout |
| *transport=`grpc`*, *`wasm`* | *Recognized in config, stubbed in v1* |

**Plugin kinds** (what it does):

| Kind | Used by | Contract method |
|---|---|---|
| `handler` (default) | `type: plugin` routes | One `Call(trigger_key, body)` |
| `auth` | `auth.<name>.type: plugin` | `Authenticate / RefreshClaims / Logout` |
| `storage` | `storage.<name>.type: plugin` | `Query(sql, params)` |
| `secrets` | `${PLUGIN:name:uri}` interpolation | `Resolve(uri)` |
| `exporter` | `observability.exporters:` | Receive batched metrics / logs / traces |

**Per-plugin config**: `command`, `address`, `timeout`, `env`,
`retries`, `retry_backoff`.

→ [Plugins concept](/guide/concepts-plugins) · [docs/plugins.md](https://github.com/luowensheng/wave/blob/main/docs/plugins.md)

---

## Scheduler

Cron jobs declared under top-level `schedule:`. Each entry is a
named job with a trigger + action + sinks.

**Triggers:** `every: 30s` (fixed interval) or `at: "07:30"` (daily
wall-clock).

**Actions:**

| `action.type:` | What |
|---|---|
| `api` | Outbound HTTP call (`ref:` named request OR inline `url`/`method`/`headers`/`body`) |
| `plugin` | Call a plugin (`plugin:` name + `trigger_key:`) |
| `storage` | Query a storage backend |

**Sinks** (`then:` list, executed in order):

| `then.type:` | What |
|---|---|
| `storage` | INSERT/UPDATE the result |
| `publish` | Broadcast over a named SSE/WS connection |
| `plugin` | Pass result to another plugin |
| `api` | POST the result to an endpoint |
| `for_each` | Loop over an array, run nested sinks per item |

**Variables**: `inputs: { name: "accum.path" }` — strict-scope
dot-path resolution. `on_error: continue|skip|abort` controls
sink failure behavior.

→ [Schedule recipe](/cookbook/schedule)

---

## `type: match` — predicate routing

Dispatch one path to N nested routes based on request envelope.

**Predicates** (one per case):

| `when:` | Matches against |
|---|---|
| `method` | HTTP verb |
| `host` | Host header |
| `ip` | Client IP |
| `header` | Map of `header-name: criteria` |
| `cookie` | Map of `cookie-name: criteria` |
| `query` | Map of `query-param: criteria` |
| `path` | Map of `path-var: criteria` |

**Operators**: `equals`, `regex`, `prefix`, `exists`.

**Route references**: `route: <id>` references another route by its
`id:` field. Routes with `id:` but no `path:` are **library-only** —
never registered as endpoints, only reachable via match cases.

→ [Multi-tenant recipe](/cookbook/multi-tenant) · [Device detection recipe](/cookbook/device-detection) · [A/B testing recipe](/cookbook/ab-testing)

---

## Composition / library system

Compose `server.yaml` from multiple files. Useful for monorepos and
shared resource libraries.

| Mechanism | Syntax | Effect |
|---|---|---|
| Module composition | `include: [{file: foo.yaml, prefix: /api/v2}]` | Merge another file's routes/resources, optionally prefixed |
| Typed libraries | Top-level `kind: storage` (or `plugins`, `auth`, `connections`, `requests`, `limits`) | Marks file as a library — not bootable as a server |
| Extern references | `extern: ./library.yaml#name` in any resource | Pull a resource by name from a library |
| Recursion guard | Hard-coded max depth 32 | Prevents include cycles |

→ [docs/composition-and-pipelines.md](https://github.com/luowensheng/wave/blob/main/docs/composition-and-pipelines.md)

---

## Built-in HTTP endpoints

| Endpoint | Always on? | What |
|---|---|---|
| `/healthz` | ✅ | Liveness probe — 200 once boot completes |
| `/readyz` | ✅ | Readiness probe — 503 until all dependencies are reachable |
| `/version` | ✅ | Build version + commit hash (JSON) |
| `/metrics` | when observability wired | Prometheus exposition format |
| `/admin` | optional | Built-in dashboard (routes, brokers, plugin status) |
| `/openapi.json` | optional | OpenAPI 3.0 spec generated from declared routes + inputs |
| `<subscribe_path>` per connection | when `connections:` declared | SSE/WS subscribe endpoint |
| `<json_discovery_route_path>` | when configured | All routes as a JSON list |
| `<html_discovery_route_path>` | when configured | All routes as an HTML table |
| `/` via `default_route:` | when configured | Server-wide catch-all |
| Framework JSON 404 envelope | ✅ when `default_route:` unset | `{"error":"page not found","path":"…"}` |

---

## HTTPS

| Field | What |
|---|---|
| `https_config.ssl_keyfile` | Path to private-key PEM |
| `https_config.ssl_certfile` | Path to cert PEM |
| `https_config.generate: true` | Auto-generate self-signed cert if files missing |
| `https_config.organization: [...]` | Org names for the generated cert |
| `https_config.dns_names: [...]` | SANs for the generated cert |
| `https_config.common_name` | CN for the generated cert |

For production, terminate TLS upstream (Fly, ALB, Caddy) and run
Wave behind it.

---

## Secrets handling

| Syntax | Resolves to |
|---|---|
| `${env:NAME}` | Process environment variable; fails fast at boot if unset |
| `${PLUGIN:name:uri}` | Call plugin `name` (kind: `secrets`) with `uri`; result inlined |

Use for `secret:`, `client_secret:`, DB DSNs, etc. — never inline
credentials in `server.yaml`.

---

## Migrations

```sh
wave migrate up   --db ./data.db --dir ./migrations
wave migrate down --db ./data.db --dir ./migrations
```

File naming: `001_create_users.up.sql`, `001_create_users.down.sql`
— numeric ordinal + description. Idempotent (tracks applied state
in `_wave_migrations` table inside the same DB).

---

## Bundler (optional)

Frontend build pipeline under `build:`:

| Field | What |
|---|---|
| `watch: true` | Hot-reload on source change (dev) |
| `dist_dir:` | Output directory (auto-served by a generated `type: static` route) |

Powered by esbuild (or equivalent). Handy if your repo has both
backend YAML and a React/Vue/Svelte frontend.

---

## CLI

| Command | What |
|---|---|
| `wave serve <file.yaml>` | Run a server |
| `wave serve-live <file.yaml>` | Run a server, hot-reload on file change |
| `wave validate <file.yaml>` | Boot-time config check (no server) |
| `wave test <suite.test.yaml>` | Functional test runner (YAML-driven, in-process) |
| `wave fmt <file.yaml>` | Canonicalize YAML formatting (`--check` for CI) |
| `wave routes <file.yaml>` | Print the route table (`--format=table\|json`) |
| `wave doctor <file.yaml>` | Pre-flight diagnostics (`--json` for CI) |
| `wave init <template> <dir>` | Scaffold a starter project (`wave init list` for templates) |
| `wave migrate up\|down --db --dir` | Apply / roll back SQLite migrations |
| `wave outbox list\|dlq\|replay --db` | Inspect and operate the durable webhook outbox |
| `wave studio` | Multi-project web UI |
| `wave completion bash\|zsh\|fish` | Shell completion script |
| `wave version` | Build version + commit hash |
| `wave help` | Usage banner |

### `wave init` templates

| Template | Best for |
|---|---|
| `api` | Minimal REST API (routes + storage + inputs) |
| `spa` | SPA + API gateway (static + backend in one) |
| `internal-tool` | Authenticated web app with admin pages |
| `plugin-starter` | JSON-RPC plugin scaffold + sample server.yaml |
| `streaming` | SSE / WebSocket event broadcaster |
| `oidc-api` | OIDC-protected API with role-based access |
| `graphql` | GraphQL endpoint over a storage backend |

---

## Functional testing (`wave test`)

YAML-driven test runner; same execution path as production. See
[the testing recipe](/cookbook/testing) for the full reference.

**Suite shape**: `import:` (server.yaml under test), `env:`,
`setup:` (skip tests if any fail), `tests:`, `teardown:`.

**Case fields**: `request: {method, path, headers, query, body, json, form}`,
`expect: {status, body, body_contains, headers, json}`,
`capture: {var: json.path}`.

**JSON-subset matching** with `"*"` wildcard for "any present value".

**Variable interpolation** via `{{.var_name}}` in path / body /
headers / query / any string leaf inside `json:`.

**CLI**: `wave test … --json` for CI envelope, `--verbose` to keep
server logs visible.

**Go embedding**: `wavetest.RunFile` (logs visible) or
`wavetest.RunFileWithOptions{Quiet: true}` (silenced).

→ [Testing recipe](/cookbook/testing) · 3 runnable suites in [`examples/apps/`](https://github.com/luowensheng/wave/tree/main/examples/apps)

---

## AI affordances

What Wave ships to make AI-assisted development reliable:

| File / surface | Use for |
|---|---|
| [`llms.txt`](https://raw.githubusercontent.com/luowensheng/wave/main/llms.txt) at repo root | LLM-readable index of docs + the four non-negotiable rules. Drop into any prompt. |
| [`.claude/skills/wave.md`](https://github.com/luowensheng/wave/blob/main/.claude/skills/wave.md) | Claude Code skill — auto-activates when user works in a Wave project |
| [`docs/server.schema.json`](https://raw.githubusercontent.com/luowensheng/wave/main/docs/server.schema.json) | JSON Schema for `server.yaml` — attach to your editor for autocomplete |
| 64 demo apps under [`examples/apps/`](https://github.com/luowensheng/wave/tree/main/examples/apps) | Concrete worked examples; LLMs reference them well |

→ [Wave + AI agents overview](/ai/) · [Token efficiency](/ai/token-efficiency) · [Editor setup](/ai/editors) · [Prompt patterns](/ai/prompts)

---

## What we don't have (honest list)

So you don't have to wonder:

- **No gRPC server** — route types only speak HTTP. Use a Go plugin
  if you need to host gRPC.
- **No raw WebSocket frames** — `connections.type: ws` exists but
  currently degrades to SSE (server-push only).
- **No GraphQL federation** — `type: graphql` is single-schema.
- **No first-class SAML** — supported via the `saml-auth` plugin,
  not built into core.
- **No ABAC / policy DSL** — RBAC is claims-only (`require_roles`
  + `require_claims`). For complex policies, use a Go plugin.
- **No hot-reload of `server.yaml`** at runtime — restart on change
  (`wave serve-live` watches and restarts).
- **No built-in user store** with password reset, account lockout,
  etc. — `auth-signup` + `auth-login` are basic. Plug in a real
  user backend for production.
- **No multi-region SQLite replication** — pair with LiteFS or move
  to Postgres-via-plugin for multi-region.
- **No telemetry / phone-home** — by design ([Privacy](/guide/privacy)).
- **No Windows-native dev shell** — the daily-dev loop assumes
  POSIX (WSL works). Windows binary builds + run.

---

## See also

- [Quickstart](/guide/quickstart) — 5-min working example
- [Tutorial](/guide/tutorial) — 30-min build of a todo API with all the trimmings
- [Cookbook](/cookbook/) — 20 copy-paste recipes
- [Comparison](/guide/comparison) — Wave vs Express / FastAPI / Gin / Caddy / Hasura
- [CLAUDE.md](https://github.com/luowensheng/wave/blob/main/CLAUDE.md) — canonical developer reference
- [`examples/apps/INDEX.md`](https://github.com/luowensheng/wave/blob/main/examples/apps/INDEX.md) — every one of the 64 demos
