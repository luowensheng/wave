# What is Wave?

Wave is an HTTP server framework where you describe the server in
YAML and the framework runs it. You declare routes, storage, plugins,
authentication, and scheduling; Wave compiles that into a working
production server with middleware, observability, and a single
binary you can `scp` to a box.

::: tip TL;DR
- **28 route types** for the boring 80% — CRUD, auth, webhooks, scheduling, SSE, files, multi-tenant routing
- **5-10× fewer tokens** than Express/FastAPI when AI-assisted ([why](/ai/token-efficiency))
- **Safe by default** — SQL injection impossible by construction, webhook signatures, CSRF, rate limits
- **Single binary deploy** — `wave serve config.yaml`, ~25 MB Docker image
- **Built-in testing** — `wave test server.test.yaml` runs YAML cases in-process
- **Fits your existing stack** — BFF for React, gateway for Node, sidekick for Python
:::

## When to use Wave

Use Wave when:

- You're building a backend that's mostly **CRUD, auth, and webhooks**
  with a small amount of custom logic.
- You want **one config file** that's reviewable by non-Go engineers
  (PMs, designers, ops).
- You need **observability, auth, and security primitives** out of the
  box and don't want to wire them yourself.
- You're pairing with an **LLM/agent** and want the model to produce
  working configs reliably.
- You want **minimal operational surface** — a binary, a YAML, and
  optional plugin binaries. No language runtime to manage.

## When NOT to use Wave

Skip Wave (or use it sparingly) when:

- Your domain is **heavily computational** (image processing,
  ML inference, real-time games). Wave is great at routing,
  validation, and persistence; it doesn't replace a worker tier.
- You **want full control of the request lifecycle** (custom HTTP/2
  push, low-level streaming patterns, custom protocol handling).
  Wave abstracts these.
- Your team is **deeply committed to a different runtime**
  (Node, Python, Rust). Wave plays well alongside them as a gateway
  but isn't a replacement.

## How Wave compares

Wave sits in the same space as:

- **Express / Fastify / FastAPI** — Wave is more declarative; less Go
  code, more YAML. Trade-off: less flexibility on edge cases.
- **Caddy** — both are declarative. Caddy is the canonical reverse
  proxy / static server; Wave goes deeper into application logic
  (storage, auth, plugins, scheduling).
- **Hasura / Supabase** — those generate APIs from a schema; Wave
  lets you hand-author the API surface. Less magic, more control.

See the [Comparison page](/guide/comparison) for a longer table.

## How a Wave server is structured

```yaml
# server.yaml
default:                # bind defaults
  port: 8080

env:                    # interpolated values
  API_KEY: { description: "Stripe secret" }

storage:                # named storage backends
  app: { type: sqlite, path: ./data.db, tables: { ... } }

plugins:                # out-of-process workers
  worker: { kind: subprocess, command: [...] }

connections:            # SSE/WebSocket brokers
  events: { type: sse, subscribe_path: /events/stream }

schedule:               # in-process cron
  daily_cleanup: { every: 24h, action: { ... }, then: [ ... ] }

routes:                 # one entry per endpoint
  - path: /users
    method: GET
    type: storage-access
    inputs: [ ... ]
    storage-access: { source: app, execute: "..." }
```

Every top-level key is a separately-resolved namespace. The
[Reference](/reference/) covers each in detail.

## Next steps

- [**Quickstart**](/guide/quickstart) — run a Wave server in 5 minutes.
- [**Tutorial**](/guide/tutorial) — build a real todo API with auth.
- [**Cookbook**](/cookbook/) — copy-paste recipes for common needs.
