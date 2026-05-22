# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

> **Pre-1.0 note:** Wave is pre-1.0. Breaking changes between 0.x minor
> versions are allowed and will be documented here. After v1.0.0 the
> normal SemVer contract applies.

## [Unreleased]

### Added
- `type: match` — declarative request router that dispatches a single
  path to one of several nested routes based on predicates (method,
  header, cookie, query, host, IP, path var). Supports `equals`,
  `regex`, `prefix`, and `exists` operators. Subsumes the earlier
  `type: methods` proposal — method dispatch is just a predicate.
- Optional `id` field on routes. Routes with `id` + `path` are
  registered as endpoints *and* referenceable from `type: match`
  cases; routes with `id` but no `path` are library-only (never
  registered, only reachable via `route: <id>` references).
- Top-level `default_route:` catch-all. When set, the route is
  mounted at `/` and answers any path no other route claims. When
  unset, the framework emits a consistent JSON 404
  (`{"error":"page not found","path":"…"}`) for both server-wide
  unmatched paths and match routes with no matching case + no default.
- `examples/apps/match-route-demo/` — end-to-end demo of the new
  route type with device detection, A/B variants, locale routing,
  and method dispatch.
- `examples/apps/cors-preflight-demo/` — proves browser CORS
  preflights work on method-bound forward routes.

### Changed
- `Server.HandleFunc` extracted its middleware chain into a reusable
  `wrapRouteMiddleware` helper so per-case sub-handlers in
  `type: match` carry their own full middleware stack (auth, inputs,
  CORS, csrf, etc.).
- CORS wrapper now answers OPTIONS preflights unconditionally when
  `cors_origins` is set — previously required an `Origin` header,
  which broke `curl` probes and same-origin checks.
- `HandleCORS` reflects the requested `Access-Control-Request-Method`
  and `Access-Control-Request-Headers` instead of hard-coding
  `GET, OPTIONS` (which broke browser preflights for any verb other
  than GET).
- `Route.Validate` accepts routes with `id` but no `path` (library-only).

### Fixed
- `infra/common/common.go`: unreachable code after early return in
  `LoadObjectFromFile` (caught by `go vet`).

### Security
- The strict-scope DataLoader path ensures every SQL value goes through
  `{{name}} → ?` parameterised binding. Documented in CLAUDE.md.

---

## [0.1.0] — Unreleased

Initial public release.

### Features
- 28 route types: `static`, `file`, `forward`, `api`, `content`,
  `storage-access`, `task`, `plugin`, `process`, `file-server`,
  `stream-publish`, `graphql`, `fetch`, `auth-login`, `auth-signup`,
  `auth-logout`, `magic-link-request`, `magic-link-consume`,
  `totp-enroll-start`, `totp-enroll-confirm`, `totp-verify`,
  `oauth-start`, `oauth-callback`, `dependencies`, `redirect`,
  `dynamic-forward`, `match`.
- Pluggable storage backends (SQLite built-in; Postgres / others via plugins).
- Plugin transports: subprocess, HTTP, long-lived.
- Connections: Server-Sent Events brokers with ring-buffer replay.
- Scheduler: cron-like jobs with `action` + `then` sinks.
- Outbox CLI: `wave outbox list|dlq|replay` for durable forwards.
- Migrations: `wave migrate up|down`.
- Doctor: `wave doctor` pre-flight diagnostics.
- Observability: Prometheus `/metrics`, OpenTelemetry traces, audit log.
- Auth: magic link, TOTP, OAuth, OIDC, RBAC (claims-based).
- Webhook signature verification: Stripe, GitHub, Slack, generic HMAC.
- Per-route middleware: CSRF, IP filter, rate limit, circuit breaker,
  response cache, body size limit, request schema validation,
  declared inputs.
- Health endpoints: `/healthz`, `/readyz`, `/version`.

### Examples
- 57 runnable demo applications under `examples/apps/`.

[Unreleased]: https://github.com/luowensheng/wave/compare/v0.1.0...HEAD
[0.1.0]: https://github.com/luowensheng/wave/releases/tag/v0.1.0
