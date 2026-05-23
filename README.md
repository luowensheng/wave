<p align="center">
  <h1 align="center">Wave</h1>
  <p align="center">
    <strong>A declarative HTTP server framework — describe your backend in YAML, ship a single binary.</strong>
  </p>
  <p align="center">
    <a href="https://github.com/luowensheng/wave/actions/workflows/ci.yml"><img alt="CI" src="https://github.com/luowensheng/wave/actions/workflows/ci.yml/badge.svg"></a>
    <a href="https://github.com/luowensheng/wave/releases"><img alt="Release" src="https://img.shields.io/github/v/release/luowensheng/wave?display_name=tag&sort=semver"></a>
    <a href="https://github.com/luowensheng/wave/blob/main/LICENSE"><img alt="License" src="https://img.shields.io/badge/license-Apache--2.0-blue.svg"></a>
    <a href="https://go.dev/"><img alt="Go" src="https://img.shields.io/badge/go-1.24+-00ADD8.svg"></a>
  </p>
  <p align="center">
    <a href="https://luowensheng.github.io/wave/"><strong>Docs</strong></a> •
    <a href="https://luowensheng.github.io/wave/guide/quickstart"><strong>Quickstart</strong></a> •
    <a href="https://luowensheng.github.io/wave/cookbook/"><strong>Cookbook</strong></a> •
    <a href="https://luowensheng.github.io/wave/ai/token-efficiency"><strong>For AI agents</strong></a>
  </p>
</p>

---

## A working JSON API in 13 lines of YAML

```yaml
storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      users: { columns: ["id INTEGER PRIMARY KEY", "name TEXT NOT NULL"] }

routes:
  - path: /users
    method: POST
    type: storage-access
    inputs: [{ name: name, source: body, type: string, required: true, min: 1 }]
    storage-access:
      source: app
      execute: "INSERT INTO users(name) VALUES ({{name}})"
      output_template: '{"id": {{.LastInsertID}}}'
```

```sh
wave serve server.yaml --port 8080
curl -X POST -d '{"name":"ada"}' http://localhost:8080/users
# {"id": 1}
```

That's a working endpoint with **input validation**, **parameterised SQL** (no
injection possible), **JSON response**, and a built-in **`/healthz`** probe. No
Go code, no `node_modules`, no Docker Compose stack. The same `server.yaml`
deploys as a single binary or a 25 MB distroless container.

## What ships in the box

**[→ Full feature inventory](https://luowensheng.github.io/wave/reference/features)** — every route type, middleware, CLI subcommand, plugin kind, in one searchable page.

| | Count | What |
|---|---:|---|
| **Route types** | 28 | `storage-access`, `task`, `match`, `forward`, `auth-login`, `magic-link-*`, `oauth-*`, `totp-*`, `webhook` (with signature verify), `stream-publish`, `schedule`, `process`, `plugin`, `graphql`, `file-server`, … |
| **Demo apps** | 64 | Self-contained `server.yaml` files under `examples/apps/` — chat, polls, todo, pastebin, multi-tenant SaaS, Stripe receiver, SSE chat, photo gallery, OIDC, SAML, audit-logged admin, ML sidekick, … |
| **Cookbook recipes** | 16 | Copy-paste patterns for the common needs |
| **CLI commands** | 11 | `serve`, `test`, `validate`, `fmt`, `routes`, `doctor`, `init`, `migrate`, `outbox`, `completion`, `studio` |
| **Auth built-in** | 6 | magic link, OAuth, OIDC, SAML (via plugin), TOTP 2FA, JWT |
| **Webhook providers** | 4 | Stripe, GitHub, Slack, generic HMAC — all with replay protection |
| **Observability** | 4 | Prometheus `/metrics`, OpenTelemetry traces, JSON access logs, append-only audit log |
| **Reliability** | 5 | Outbox CLI, circuit breaker, rate limiter, body-size limits, response cache |
| **Deploy targets** | 4 | macOS / Linux / Windows binaries + distroless Docker image |

## Why Wave?

### 🚀 Ship faster

Most backends are 80% boilerplate — request parsing, validation, DB calls,
auth wiring, middleware ordering. Wave does that 80% declaratively. You write
Go (or any language, as a plugin) only where it actually matters.

### 🤖 5-10× fewer tokens for AI-assisted development

The same JSON API endpoint:

| Stack | Lines | Tokens |
|---|---:|---:|
| **Wave** | **13** | **~140** |
| FastAPI + Pydantic | 24 | ~360 |
| Gin (Go) | 38 | ~440 |
| Express + Zod + Prisma | 38 | ~520 |

More features per Cursor request, more state per Claude context window, fewer
hallucinations. Wave ships [`llms.txt`](llms.txt), a [Claude Code
skill](.claude/skills/wave.md), and a JSON Schema for `server.yaml` so AI
editors auto-complete and produce working configs first try. See
[the full comparison](https://luowensheng.github.io/wave/ai/token-efficiency).

### 🔒 Safe by default — not as an afterthought

- **SQL injection: impossible.** Every value goes through `{{name}} → ?`
  parameterised binding. The framework rejects unsafe alternatives.
- **CSRF**, **webhook signatures** (Stripe / GitHub / Slack), **rate limits**,
  **circuit breakers**, **body-size limits**, **request-schema validation**,
  **secure headers** — all wired into the middleware chain.
- **RBAC** via claims, **forward auth** for delegation, **audit log** for
  every mutation.

### 📦 Real production primitives

- **`/healthz` + `/readyz`** built in
- **Prometheus** `/metrics` + **OpenTelemetry** traces
- **Outbox CLI** for durable webhook delivery with retry + DLQ
- **Migrations** (`wave migrate up`)
- **Doctor** (`wave doctor --json`) for pre-flight checks
- **Functional test runner** (`wave test`) — YAML-driven, in-process, no port

### 🧪 Testable end-to-end

```yaml
# server.test.yaml
import: server.yaml
tests:
  - name: create user
    request: { method: POST, path: /users, json: { name: ada } }
    expect:  { status: 200, json: { id: "*" } }
    capture: { id: json.id }
  - name: read it back
    request: { method: GET, path: /users/{{.id}} }
    expect:  { status: 200, json: { name: ada } }
```

```sh
wave test server.test.yaml --json    # CI-friendly, in-process, no port binding
```

[Full testing recipe →](https://luowensheng.github.io/wave/cookbook/testing)

### 🧩 Fits your existing stack

Wave isn't a replacement for React, Node, or Python. It's a complement:

- [**React / Next.js + Wave backend**](https://luowensheng.github.io/wave/cookbook/react-wave) — auth + persistence in YAML, your frontend stays on Vercel
- [**Wave in front of an Express service**](https://luowensheng.github.io/wave/cookbook/node-gateway) — gateway pattern for auth, rate-limit, audit, webhook signatures
- [**Wave + Python ML service**](https://luowensheng.github.io/wave/cookbook/python-sidekick) — wrap a FastAPI/model server with auth + SSE + 202-task patterns
- [**Migrate from Express incrementally**](https://luowensheng.github.io/wave/cookbook/migrate-from-express) — route-at-a-time, no big-bang

## Pick your path

| You are… | Start here |
|---|---|
| **Trying it for the first time** | [Quickstart (5 min)](https://luowensheng.github.io/wave/guide/quickstart) |
| **Building a real app** | [Tutorial: build a todo API (30 min)](https://luowensheng.github.io/wave/guide/tutorial) |
| **An indie hacker** | [`wave init api`](https://luowensheng.github.io/wave/guide/quickstart) — scaffolded project with auth + Docker + Fly.io ready |
| **A backend engineer** | [Comparison vs Express / FastAPI / Gin](https://luowensheng.github.io/wave/guide/comparison) |
| **A platform / SRE engineer** | [Production checklist](https://luowensheng.github.io/wave/guide/deploy-checklist) + [Observability](https://luowensheng.github.io/wave/guide/concepts-observability) |
| **An AI agent builder** | [Token efficiency](https://luowensheng.github.io/wave/ai/token-efficiency) + [Claude skill](https://luowensheng.github.io/wave/ai/claude-code) |
| **Adding Wave to an existing app** | [Wave in your stack](https://luowensheng.github.io/wave/guide/wave-in-your-stack) |

## Install

```bash
# Pre-built binaries (macOS / Linux / Windows)
curl -sSfL https://luowensheng.github.io/wave/install.sh | sh

# Pin a version
curl -sSfL https://luowensheng.github.io/wave/install.sh | sh -s -- v0.1.0

# Or via Go (latest main, includes built-in SQLite)
go install github.com/luowensheng/wave/orchestrator@latest

# Or via Docker (sqlite-capable)
docker run --rm -p 8080:8080 \
  -v $(pwd)/server.yaml:/server.yaml \
  ghcr.io/luowensheng/wave:latest serve /server.yaml --port 8080
```

> Released binaries are built `nosqlite` for cross-platform simplicity.
> Use the Docker image or `go install` for built-in SQLite. Homebrew
> formula lands shortly.

## CLI at a glance

```sh
wave serve server.yaml --port 8080        # run a server
wave test  server.test.yaml               # run a YAML test suite
wave validate server.yaml                 # boot-time config check (no server)
wave fmt server.yaml --check              # CI-safe yaml formatter
wave doctor server.yaml --json            # live diagnostics
wave routes server.yaml --format=json     # print the route table
wave init api ./my-project                # scaffold a starter
wave migrate up --db ./data.db --dir ./migrations
wave outbox list --db ./outbox.db         # operate the durable outbox
wave completion bash|zsh|fish             # shell completion
```

## The 30-second taste

```sh
git clone https://github.com/luowensheng/wave.git
cd wave

# Pick any of the 64 demos
go run ./orchestrator serve examples/apps/url-shortener/server.yaml --port 8102
curl http://localhost:8102/healthz
# {"status":"ok",…}

# Or run its test suite
go run ./orchestrator test examples/apps/url-shortener/server.test.yaml
#   PASS [test] built-in /healthz returns JSON with status ok (200, 1ms)
#   PASS [test] unknown path returns framework 404 envelope (404, 0s)
#   PASS [test] POST /shorten validates slug pattern (400, 0s)
#   …
#   8 passed, 0 failed, 0.00s
```

## What it's not

- Not a frontend framework (it serves your React/Vue/Svelte build, but
  doesn't render it).
- Not a service mesh (it sits at L7, in your app; pair with Istio/Linkerd
  if you need mTLS).
- Not a workflow engine (it has a scheduler; use Temporal/Airflow if you
  need durable multi-step workflows).
- Not a replacement for your domain code — it's the boring parts done
  declaratively so you can focus on the parts that aren't.

## Documentation

- **[luowensheng.github.io/wave](https://luowensheng.github.io/wave/)** — docs site (45 pages)
- **[CLAUDE.md](CLAUDE.md)** — full developer guide (architecture, conventions, every YAML key)
- **[docs/](docs/)** — reference docs (storage, auth, plugins, composition, bundler)
- **[examples/apps/](examples/apps/)** — 64 runnable demos
- **[llms.txt](llms.txt)** — LLM-friendly index for AI agents

## Community

- **[GitHub Discussions](https://github.com/luowensheng/wave/discussions)** — questions, ideas, show & tell
- **[Issues](https://github.com/luowensheng/wave/issues)** — bug reports and feature requests
- Discord — *coming soon*

## Status

**Pre-1.0.** Breaking changes are allowed between 0.x minors and are
documented in [CHANGELOG.md](CHANGELOG.md). Production usage is welcome —
pin a version and read the CHANGELOG before upgrading.

## Privacy

**Wave never phones home.** No telemetry, no analytics, no remote config
fetches. The single binary only contacts services *you* configure.

## Contributing

PRs welcome. Start with [CONTRIBUTING.md](CONTRIBUTING.md) for the process
and [CLAUDE.md](CLAUDE.md) for the architecture. Good first issues:
[`good-first-issue`](https://github.com/luowensheng/wave/labels/good-first-issue).

## License

Apache-2.0 — see [LICENSE](LICENSE).
