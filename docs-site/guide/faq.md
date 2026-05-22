# FAQ

## Can I write custom Go code?

Yes — via [plugins](/guide/concepts-plugins). A plugin is an
out-of-process binary that speaks a tiny JSON contract over stdin/
stdout, HTTP, or a long-lived socket. You can write plugins in Go,
Python, Node, Rust, or anything that can speak JSON.

The trade-off vs in-process code: plugins are slower (IPC overhead)
but isolated (a crashy plugin can't take down the server, and you
can write plugins in any language).

## Why YAML and not JSON / TOML / HCL?

YAML supports comments and multi-line strings, which matter for SQL
templates. LLMs also produce YAML more reliably than HCL, in our
experience. JSON Schema validates YAML structurally so you still get
editor auto-complete.

## How does Wave compare to Hasura / Supabase?

See the [Comparison page](/guide/comparison). Short version: Hasura
generates an API from a database schema; Wave lets you hand-author
each endpoint. Different tools for different ergonomic preferences.

## Is Wave production-ready?

Wave is pre-1.0. The runtime is in production use at a small scale.
Use it if:

- You can pin a version and read CHANGELOG before upgrading.
- You're comfortable filing bug reports against an early-stage
  project.
- Your traffic isn't bursting into the 10k+ rps range (we haven't
  benchmarked at that scale).

Avoid it if you need formal SLAs, FedRAMP/SOC2 compliance, or
24/7 vendor support — those land after v1.0.

## What about gRPC / GraphQL / WebSockets?

- **gRPC**: not natively supported. Use a Go plugin.
- **GraphQL**: `type: graphql` exists for simple cases. For complex
  schemas use a dedicated GraphQL server behind a `type: forward`.
- **WebSockets**: SSE is first-class via `type: stream-publish` and
  `connections:`. For raw WebSockets, plug in a separate WS server
  and proxy via `type: forward`.

## Does Wave phone home?

**No.** Wave never sends telemetry, never fetches remote config, never
contacts any service except the ones you configure. The single
binary makes this trivial to verify with `strace`/`dtrace` if you
want.

## How do I migrate from Express / FastAPI / Gin?

Start with the [Cookbook](/cookbook/). Most patterns translate
straightforwardly. The big mindset shift: business logic that
would normally be a Go function becomes a YAML route. Wave is at
its strongest when 80% of your endpoints are declarative and only
the special cases need code (via plugins).

## How do I run plugins in development?

Plugin binaries are referenced by absolute path in `server.yaml`:

```yaml
plugins:
  my_plugin:
    kind: subprocess
    command: ["/tmp/wave-my-plugin"]
```

Build the plugin separately (`go build -o /tmp/wave-my-plugin ./cmd/plugin`),
then run Wave. Demos in `examples/plugins/` show this pattern.

## What database backends are supported?

- **SQLite** is built in (default).
- **Postgres / MySQL / others** are via storage plugins. The
  `examples/apps/postgres-*` demos show the contract.

## How do I deploy Wave?

The binary + your `server.yaml` is the entire deploy. See:

- [Docker](/guide/deploy-docker)
- [Fly.io](/guide/deploy-fly)
- [Production checklist](/guide/deploy-checklist)

## I have a question not answered here

Best: [GitHub Discussions](https://github.com/luowensheng/wave/discussions).
Bug? File an [Issue](https://github.com/luowensheng/wave/issues/new/choose).
