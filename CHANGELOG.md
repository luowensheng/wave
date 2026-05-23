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
- `Server.BuildHandler(ctx)` extracted from `Server.Start` so the
  full handler chain can be built without binding a port. Used by
  the new `wave test` command. `Start` now calls `BuildHandler`
  then wraps in `http.Server` for `ListenAndServe`. Pure code-move
  — existing tests pass unchanged.
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
- Added unit-test coverage for `dynamic_forward`'s SSRF guardrails:
  RFC1918 ranges, loopback (127.0.0.1, ::1), link-local
  (169.254.169.254 — AWS metadata service), multicast (224.0.0.0/4),
  and the unspecified address (0.0.0.0). Allowed-domains check is
  case-insensitive and whitespace-trimmed.
- Added path-traversal regression tests for `file_server` covering
  `../leak.txt`, `sub/../../leak.txt`, and `./../leak.txt` patterns.

### Tests
- Unit tests added for 8 previously untested usecases:
  `auth_login`, `auth_signup`, `auth_logout`, `api`, `redirect`,
  `run_process`, `dynamic_forward`, `file_server`. Test coverage
  on critical route handlers now matches the bar set by
  `usecases/match/config_test.go`.

### CLI
- `wave test <suite.test.yaml>` — functional test runner. Loads a
  `.test.yaml`, boots the imported `server.yaml` in-process (no
  port binding, via the new `Server.BuildHandler` API), and runs
  YAML-defined request/assert cases against it. Supports `setup`,
  `tests`, `teardown` phases; variable capture between cases with
  `{{.var}}` interpolation in path/headers/body/query; strict JSON
  subset matching with the `"*"` wildcard. `--json` for CI output,
  `--verbose` to keep server logs visible.
  By default, server boot prints + per-request access logs are
  silenced via `syscall.Dup2` (Unix-only — no-op on Windows) so
  the pass/fail report and `--json` envelope are clean. CI-safe
  exit codes: 0 = all pass, 1 = test failures, 2 = bad invocation.
  Three runnable suites ship with the framework:
  `examples/apps/{url-shortener,kv-store,pastebin}/server.test.yaml`
  totalling 22 cases that cover validation, capture+interpolate,
  raw-body upload, teardown, and PK-violation 500s.
  Go embedding API: `wavetest.RunFile` (logs visible, fits
  `go test -v`) or `wavetest.RunFileWithOptions{Quiet: true}` for
  silent runs. 12 self-tests in `infra/wavetest/` including an
  end-to-end test that boots a real Server through BuildHandler.
- `wave fmt <file.yaml> [--check | --stdout]` — canonicalize YAML
  formatting via yaml.v3 round-trip. `--check` exits non-zero if
  the file would be reformatted (CI / pre-commit hook).
- `wave doctor --json` — machine-readable doctor output for CI.
- `wave completion bash|zsh|fish` — shell completion scripts with
  per-subcommand value completion.
- `wave help` / `wave --help` / `wave -h` — top-level usage banner.
- `wave version` now reports the linker-injected build version and
  short commit hash. Defaults to `dev/none` on local builds; CI
  injects `${GITHUB_REF_NAME}` and the short SHA via ldflags.

### Release engineering
- `.goreleaser.yml`: 5-platform build matrix
  (linux/darwin/windows × amd64/arm64), archives with checksums.
  Cosign keyless signing via sigstore OIDC (no key management).
  SBOM via syft attached to releases. Multi-arch Docker images
  published to ghcr.io/luowensheng/wave on every tag.
- `.github/workflows/release.yml`: triggered on `git tag v*`, runs
  goreleaser end-to-end (build, sign, SBOM, GHCR push, GitHub
  Release with formatted notes).
- `install.sh` at repo root: POSIX shell script that detects
  OS/arch, downloads the matching tarball from the latest GitHub
  Release, and installs to `/usr/local/bin`. Pinnable via
  `WAVE_VERSION=v0.1.0` or `sh -s -- v0.1.0`.
- `Dockerfile` hardened: distroless nonroot base, multi-stage,
  `VERSION` / `COMMIT` build args wired to `wave version` output.
  HEALTHCHECK removed (distroless has no shell — orchestrators
  define their own probes against /healthz).
- `.dockerignore`: trim build context for faster Docker builds.

### Documentation
- VitePress docs site under `docs-site/`, auto-deployed to
  https://luowensheng.github.io/wave/ on push to main.
- Cookbook recipes for JSON API, multi-tenant routing, device
  detection, and CORS preflight.
- Comparison page (vs Gin, Echo, Caddy, Express, Fastify, FastAPI,
  Hasura, Supabase, PocketBase, K8s Ingress) — honest, no
  marketing voice.
- `llms.txt` at repo root for LLM-friendly discovery.
- `.claude/skills/wave.md` — Claude Code skill primed with the
  four non-negotiable rules and the add-a-route-type checklist.

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
