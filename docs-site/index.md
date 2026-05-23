---
layout: home

hero:
  name: Wave
  text: Declarative HTTP server framework
  tagline: 'Describe your backend in YAML. Ship a single binary. 5-10× fewer tokens to build with AI than Express / FastAPI / Gin.'
  actions:
    - theme: brand
      text: Quickstart (5 min)
      link: /guide/quickstart
    - theme: alt
      text: Tutorial (30 min)
      link: /guide/tutorial
    - theme: alt
      text: View on GitHub
      link: https://github.com/luowensheng/wave

features:
  - icon: 📜
    title: 28 route types
    details: 'CRUD, auth (magic-link / OAuth / OIDC / SAML / TOTP), webhooks, scheduling, SSE, file uploads, multi-tenant routing, GraphQL, plugin handoff — all declarative.'
    link: /reference/
    linkText: Reference
  - icon: 🤖
    title: Built for the AI-assisted era
    details: '5-10× fewer tokens per feature than Express/FastAPI. Ships with llms.txt, a Claude Code skill, and a JSON Schema for editor auto-complete. AI agents produce working configs first try.'
    link: /ai/token-efficiency
    linkText: Why
  - icon: 🔒
    title: Safe by default
    details: 'Parameterised SQL (injection impossible by construction), CSRF, webhook signatures, rate limits, circuit breakers, body limits, audit log — wired in, not bolted on.'
    link: /guide/concepts-auth
    linkText: Auth
  - icon: 🧪
    title: YAML-driven testing
    details: '`wave test server.test.yaml` boots the full handler chain in-process (no port binding), runs YAML cases, asserts JSON-subset shape with capture+interpolate. CI-friendly.'
    link: /cookbook/testing
    linkText: How
  - icon: 📦
    title: Single binary deploy
    details: '`wave serve config.yaml` is the entire deploy story. macOS/Linux/Windows binaries, distroless Docker (~25 MB), Fly.io / Railway / k8s ready. No language runtime to manage.'
    link: /guide/deploy-docker
    linkText: Deploy
  - icon: 🧩
    title: Fits your existing stack
    details: 'BFF for React/Next, gateway in front of Node, sidekick to a Python ML service. Not a replacement — a complement. Migrate from Express incrementally.'
    link: /guide/wave-in-your-stack
    linkText: Patterns
  - icon: 📊
    title: Production primitives
    details: 'Prometheus /metrics, OpenTelemetry traces, JSON access logs, append-only audit log, durable outbox CLI, migrations, /healthz + /readyz. The boring infrastructure handled.'
    link: /guide/concepts-observability
    linkText: Observability
  - icon: 🔌
    title: Extensible by plugin
    details: 'Out-of-process plugins speak a small JSON contract — write them in any language. Storage backends, secrets, auth providers, observability exporters.'
    link: /guide/concepts-plugins
    linkText: Plugins
  - icon: ✋
    title: Never phones home
    details: 'No telemetry, no analytics, no remote config. The single binary only contacts services you configure. Verify with `strace`.'
    link: /guide/privacy
    linkText: Privacy
---

## See it in 13 lines

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

That endpoint has **input validation**, **parameterised SQL**, **JSON
response**, and a built-in `/healthz` probe. No Go code, no `node_modules`,
no Docker Compose stack.

[Continue with the Quickstart →](/guide/quickstart)

## Pick your path

Wave has different shapes depending on who you are. Start in the spot that
matches you.

| You are… | Read this |
|---|---|
| **Trying it for the first time** | [Quickstart (5 min)](/guide/quickstart) |
| **Ready to build something real** | [Tutorial: build a todo API (30 min)](/guide/tutorial) |
| **An indie hacker / weekend project** | [`wave init api`](/guide/install) → scaffolded project, deploy buttons |
| **A backend engineer evaluating** | [Comparison vs Express/FastAPI/Gin/Caddy/Hasura](/guide/comparison) |
| **A platform / SRE team** | [Production checklist](/guide/deploy-checklist) + [Observability](/guide/concepts-observability) |
| **A startup adding a new service** | [Wave in your stack](/guide/wave-in-your-stack) |
| **Adding Wave to an existing Node app** | [Migrate from Express incrementally](/cookbook/migrate-from-express) |
| **An AI agent builder** | [Token efficiency](/ai/token-efficiency) + [Claude skill](/ai/claude-code) |
| **Just here to learn the patterns** | [Cookbook (16 recipes)](/cookbook/) |

## Same feature, four frameworks

The same JSON API endpoint — POST a user, validate, INSERT, return `{id}`:

| Framework | Lines | Tokens (Cursor/Claude) | × Wave |
|---|---:|---:|---:|
| **Wave** | **13** | **~140** | **1.0×** |
| FastAPI + Pydantic | 24 | ~360 | 2.6× |
| Gin (Go) | 38 | ~440 | 3.1× |
| Express + Zod + Prisma | 38 | ~520 | 3.7× |

More features per Cursor request, more state per Claude context window,
fewer hallucinations. [See the full comparison →](/ai/token-efficiency)

## What's actually in the box

- **28 route types** covering CRUD, auth, webhooks, scheduling, SSE, files, GraphQL, multi-tenant routing, …
- **64 runnable demo apps** under [`examples/apps/`](https://github.com/luowensheng/wave/tree/main/examples/apps) — including chat, polls, todo, pastebin, multi-tenant SaaS, Stripe receiver, SSE chat, photo gallery, OIDC, SAML, audit-logged admin
- **3 ready-to-run wavetest suites** in `examples/apps/{url-shortener,kv-store,pastebin}/server.test.yaml`
- **11 CLI commands** including `serve`, `test`, `validate`, `fmt`, `doctor`, `init`, `migrate`, `outbox`
- **Auth providers**: magic link, OAuth (Google/GitHub/Apple), OIDC, SAML (plugin), TOTP, JWT
- **Webhook providers** with signature verification: Stripe, GitHub, Slack, generic HMAC
- **Observability**: Prometheus `/metrics`, OpenTelemetry traces, JSON access logs, audit log
- **Reliability**: outbox CLI, circuit breaker, rate limiter, body-size limits, response cache
- **Deploy**: macOS / Linux / Windows binaries + distroless Docker image (~25 MB)
- **Single binary**: `wave serve config.yaml` — no language runtime

## YAML-driven functional tests

Same execution path as production, no port binding, runs in <10 ms:

```yaml
# server.test.yaml
import: server.yaml

tests:
  - name: create item
    request: { method: POST, path: /items, json: { name: laptop } }
    expect:  { status: 200, json: { id: "*" } }      # "*" = any present value
    capture: { id: json.id }                          # save for next case

  - name: read it back
    request: { method: GET, path: /items/{{.id}} }
    expect:  { status: 200, json: { name: laptop } }
```

```sh
wave test server.test.yaml --json   # CI-ready
```

[Full testing recipe →](/cookbook/testing)

## Install

```sh
# Pre-built binaries — macOS / Linux / Windows
curl -sSfL https://luowensheng.github.io/wave/install.sh | sh

# Or via Go (latest main, with built-in SQLite)
go install github.com/luowensheng/wave/orchestrator@latest

# Or via Docker
docker run --rm -p 8080:8080 \
  -v $(pwd)/server.yaml:/server.yaml \
  ghcr.io/luowensheng/wave:latest serve /server.yaml --port 8080
```

[Detailed install guide →](/guide/install)

## Status

**Pre-1.0.** Breaking changes allowed between 0.x minors, documented in
[CHANGELOG](https://github.com/luowensheng/wave/blob/main/CHANGELOG.md).
Production usage welcome — pin a version, read CHANGELOGs before upgrading.

Apache-2.0 licensed. No telemetry, ever.
