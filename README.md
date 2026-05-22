<!--
This README is intentionally short. The goal of this file is to make
a new visitor either commit to spending an hour on Wave, or rule it
out — within 60 seconds. Anything that doesn't serve that goal lives
in docs/.

Future placeholders to replace once they exist:
  install.sh URL          → wave.dev or a stable GitHub Pages url
  Homebrew tap            → publish luowensheng/homebrew-wave
  Discord invite          → currently disabled; uncomment when ready
-->

<p align="center">
  <!-- TODO: add a logo here. assets/logo.svg recommended. -->
  <h1 align="center">Wave</h1>
  <p align="center">
    <strong>A declarative HTTP server framework — define your backend in YAML, ship a single binary.</strong>
  </p>
  <p align="center">
    <a href="https://github.com/luowensheng/wave/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/luowensheng/wave/actions/workflows/ci.yml/badge.svg"></a>
    <a href="https://github.com/luowensheng/wave/releases"><img alt="Release" src="https://img.shields.io/github/v/release/luowensheng/wave?display_name=tag&sort=semver"></a>
    <a href="https://github.com/luowensheng/wave/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Apache--2.0-blue.svg"></a>
    <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/go-1.24+-00ADD8.svg"></a>
  </p>
</p>

---

## What is Wave?

Wave turns a YAML file into a production HTTP server.

```yaml
# server.yaml
storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      users:
        columns:
          - id         INTEGER PRIMARY KEY
          - name       TEXT NOT NULL
          - created_at TEXT NOT NULL DEFAULT (datetime('now'))

routes:
  - path: /users
    method: POST
    type: storage-access
    inputs:
      - { name: name, source: body, type: string, required: true, min: 1 }
    storage-access:
      source: app
      execute: "INSERT INTO users(name) VALUES ({{name}})"
      output_template: '{"id": {{.LastInsertID}}}'

  - path: /users/{id}
    method: GET
    type: storage-access
    inputs:
      - { name: id, source: path, type: int, required: true }
    storage-access:
      source: app
      execute: "SELECT * FROM users WHERE id = {{id}} LIMIT 1"
      if_empty_status: 404
      output_template: '{{toJSON .Data}}'
```

```bash
wave serve server.yaml --port 8080
curl -X POST -d '{"name":"ada"}' http://localhost:8080/users
# {"id": 1}
```

That's a working JSON API with input validation, SQL parameterisation,
and 404 handling. No Go code required.

## Why Wave?

- **Declarative.** YAML for the boring 80% of a backend — CRUD, auth,
  webhooks, scheduling, file uploads, SSE — leaves you free to write
  Go only where it matters.
- **Single binary.** `wave serve config.yaml` is the entire deploy
  story. No language runtime, no framework dependencies, no Docker
  Compose stack.
- **Safe by default.** Parameterised SQL bindings, CSRF wrappers,
  webhook signature verification (Stripe / GitHub / Slack), per-route
  rate limits and circuit breakers — wired into the middleware chain,
  not bolted on later.
- **Extensible where it counts.** Plugins for storage backends,
  secrets, auth providers, and observability exporters. Plugins are
  out-of-process binaries, so they can be written in any language.
- **AI-agent friendly.** YAML is something LLMs can write reliably.
  We ship JSON schemas, an `llms.txt` index, and a Claude Code skill
  so AI assistants can produce working Wave configs out of the box.

## Install

```bash
# Recommended — pre-built binaries (macOS / Linux / Windows)
curl -sSfL https://wave.dev/install.sh | sh           # <-- TODO: host install.sh

# Or via Homebrew
brew install luowensheng/tap/wave                      # <-- TODO: publish formula

# Or via Go
go install github.com/luowensheng/wave/orchestrator@latest

# Or via Docker
docker run --rm -p 8080:8080 \
  -v $(pwd)/server.yaml:/server.yaml \
  ghcr.io/luowensheng/wave:latest serve /server.yaml --port 8080
```

## Quickstart (30 seconds)

```bash
# 1. Clone the repo for an example to copy
git clone https://github.com/luowensheng/wave.git
cd wave

# 2. Run a demo
go run ./orchestrator serve examples/apps/url-shortener/server.yaml --port 8102

# 3. Hit it
curl http://localhost:8102/healthz
```

A full step-by-step tutorial — build a todo API with auth, validation,
and deploy — lives at `docs/tutorial/`.

## What can it do?

Wave ships **28 route types** out of the box:

| Category | Route types |
|---|---|
| **Data** | `storage-access` (single + pipeline), `api`, `content` |
| **Files** | `file`, `static`, `file-server` |
| **Proxy** | `forward`, `dynamic-forward`, `fetch` |
| **Auth** | `auth-login`, `auth-signup`, `auth-logout`, `magic-link-request`, `magic-link-consume`, `totp-enroll-start`, `totp-enroll-confirm`, `totp-verify`, `oauth-start`, `oauth-callback` |
| **Realtime** | `stream-publish`, `task` (background jobs with SSE) |
| **Routing** | `match` (predicate-based dispatch), `redirect` |
| **Other** | `plugin`, `graphql`, `process`, `dependencies` |

Plus: a scheduler with cron + action + then-sinks, an outbox CLI for
durable forwards, plugin-based observability fanout, OIDC + OAuth +
SAML (via plugin), per-route response cache, IP allow/deny, body-size
limits, request-schema validation, audit logging.

See **[examples/apps/INDEX.md](examples/apps/INDEX.md)** for **57
runnable demo applications**.

## Documentation

- **[CLAUDE.md](CLAUDE.md)** — full developer guide (architecture, conventions, recipes)
- **[docs/](docs/)** — reference docs: storage, auth, plugins, composition, bundler
- **[examples/apps/](examples/apps/)** — 57 self-contained demos
- **[examples/composition/](examples/composition/)** — modular `server.yaml` with `include:` / `extern:`

## Community

- **[GitHub Discussions](https://github.com/luowensheng/wave/discussions)** — questions, ideas, show & tell
- **[Issues](https://github.com/luowensheng/wave/issues)** — bug reports and feature requests
- Discord — *coming soon*

## Status

**Pre-1.0.** Breaking changes are allowed between 0.x minors and are
documented in [CHANGELOG.md](CHANGELOG.md). Production usage is
welcome — just pin a version and read the CHANGELOG before upgrading.

## Privacy

**Wave never phones home.** No telemetry, no analytics, no remote
config fetches. The binary you run only contacts the services *you*
configure.

## Contributing

PRs welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md) for the
process and [CLAUDE.md](CLAUDE.md) for the architecture.

Good first issues: look for the [`good-first-issue`](https://github.com/luowensheng/wave/labels/good-first-issue) label.

## License

Apache-2.0 — see [LICENSE](LICENSE).
