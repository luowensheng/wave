---
layout: home

hero:
  name: Wave
  text: Declarative HTTP server framework
  tagline: Define your backend in YAML. Ship a single binary.
  actions:
    - theme: brand
      text: Quickstart
      link: /guide/quickstart
    - theme: alt
      text: View on GitHub
      link: https://github.com/luowensheng/wave
    - theme: alt
      text: Cookbook
      link: /cookbook/

features:
  - icon: 📜
    title: Declarative
    details: 28 route types — CRUD, auth, webhooks, scheduling, SSE, file uploads — all configured in YAML. Write Go only where it matters.
  - icon: 📦
    title: Single binary
    details: '`wave serve config.yaml` is the entire deploy story. No language runtime, no framework dependencies, no Docker Compose stack.'
  - icon: 🔒
    title: Safe by default
    details: Parameterised SQL bindings, CSRF, webhook signatures (Stripe / GitHub / Slack), per-route rate limits and circuit breakers. Wired into middleware, not bolted on.
  - icon: 🧩
    title: Extensible
    details: Out-of-process plugins for storage, secrets, auth, observability. Plugins speak a tiny JSON contract — write them in any language.
  - icon: 🤖
    title: AI-agent friendly
    details: YAML is something LLMs write reliably. Ships with JSON schema, llms.txt, and a Claude Code skill so AI assistants produce working configs.
  - icon: ⚡
    title: Production-ready
    details: Prometheus metrics, OpenTelemetry traces, audit log, outbox CLI, /healthz + /readyz, migrations, secrets, RBAC. The boring infrastructure handled.
---

## A working API in 10 lines of YAML

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
    inputs:
      - { name: name, source: body, type: string, required: true }
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

[Continue with the Quickstart →](/guide/quickstart)
