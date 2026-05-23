# Plugins

Plugins are out-of-process workers that extend Wave. They speak a
small JSON contract over stdin/stdout, HTTP, or a long-lived socket.

**Why out-of-process?**

- A misbehaving plugin can't take down the server.
- Plugins can be written in any language.
- Plugins are versioned and shipped separately from Wave.

## The contract

Every plugin speaks the same JSON request/response shape:

```jsonc
// Request (Wave → plugin)
{
  "trigger_key": "chat",
  "metadata": { ... },
  "body": <raw JSON>
}

// Response (plugin → Wave)
{
  "status": 200,
  "headers": { "Content-Type": "application/json" },
  "body": <raw JSON or text>
}
```

`trigger_key` tells the plugin which action to perform (one plugin
can handle many — e.g. a single `db` plugin handles `query`,
`insert`, `migrate`).

## Three transports

| `kind` | When to use |
|---|---|
| `subprocess` | Stateless per-request, e.g. a Python script |
| `http` | Plugin is already a long-running HTTP server |
| `longlived` | Stateful per-process worker, persistent connections |

### Subprocess (simplest)

```yaml
plugins:
  echo:
    kind: subprocess
    command: ["python3", "echo.py"]
```

Per request: Wave spawns the command, writes the JSON request to
stdin, reads JSON from stdout, kills the process. Slow but trivial.

Minimal Python implementation:

```python
import sys, json
req = json.loads(sys.stdin.read())
body = json.loads(req["body"])
print(json.dumps({
    "status": 200,
    "headers": {"Content-Type": "application/json"},
    "body": {"echo": body}
}))
```

### HTTP (proxy)

```yaml
plugins:
  remote:
    kind: http
    url: http://localhost:9000
    timeout: 5s
```

Per request: Wave POSTs to the URL with the same JSON envelope.
Useful when the worker is an existing HTTP server in your stack.

### Long-lived

```yaml
plugins:
  model:
    kind: longlived
    command: ["python3", "worker.py"]
```

Per request: Wave writes a framed message to the long-running
process's stdin and reads the framed response. Avoids spawn overhead.
The plugin must implement the [framed protocol](https://github.com/luowensheng/wave/blob/main/docs/plugins.md#long-lived).

## Calling a plugin from a route

The simplest case — `type: plugin`:

```yaml
routes:
  - path: /api/echo
    method: POST
    type: plugin
    plugin:
      name: echo
      trigger_key: echo
```

For background work, [`type: task`](/cookbook/background-tasks).

For scheduled work, [`schedule:` with `action: { type: plugin }`](/cookbook/schedule).

## What plugins are good for

- **Domain code Wave can't express in YAML** — anything stateful,
  complex, ML-bound, or platform-specific.
- **Custom storage / secrets backends** — Postgres, Vault, Consul,
  custom KV.
- **LLM/AI workers** — long-running model server, queue consumer.
- **Auth providers** — SAML, LDAP, custom JWT issuer.
- **Observability exporters** — push metrics/traces to a SaaS that
  doesn't have an OTel collector.

## What plugins are NOT for

- **Per-request transformation** — that's a route type. Adding
  YAML > forking processes.
- **Authentication checks** — use `auth:` and the built-in providers.

## Plugin starter template

There's a starter scaffold:

```sh
wave init plugin-starter ./my-plugin
cd my-plugin
go build -o /tmp/wave-my-plugin .
wave serve server.yaml
```

See the [`plugin-starter`](https://github.com/luowensheng/wave/tree/main/examples/plugins)
template for the wire format and a working Go subprocess plugin.

## Plugin kinds beyond `type: plugin` route

The same plugin contract is reused across Wave:

- **Storage**: `storage.<name>.type: plugin` — plugin handles SQL.
- **Secrets**: `${PLUGIN:vault:db_password}` — plugin resolves on demand.
- **Auth**: SAML/LDAP/custom auth as a plugin.
- **Observability exporters**: push metrics out.

Same JSON envelope; different trigger_keys.

## See also

- [docs/plugins.md](https://github.com/luowensheng/wave/blob/main/docs/plugins.md)
  — full contract spec
- [Background tasks](/cookbook/background-tasks) — the most common
  use of `type: plugin` chained with SSE
- Demos: [`plugin-starter`](https://github.com/luowensheng/wave/tree/main/examples/apps/multi-plugin-stack)
