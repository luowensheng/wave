# Plugin contract reference

The full wire-format spec for every plugin kind. Use this when
porting a plugin to a non-Go language, debugging a misbehaving
plugin, or auditing what Wave actually sends.

Companion to the [Build a plugin recipe](/cookbook/build-plugin) and
the high-level [Plugins concept](/guide/concepts-plugins).

## Transports

Three transports; same JSON shape on top.

### `process` (one-shot subprocess)

Each call: Wave spawns the binary, writes the request JSON to stdin,
reads the response JSON from stdout, kills the process.

- **Input**: JSON object on stdin (terminated by EOF; no framing)
- **Output**: JSON object on stdout
- **Errors**: stderr (logged by Wave), exit code non-zero
- **Lifecycle**: one process per request
- **Per-call config**: none — every request looks the same

This is the simplest contract and what most one-shot plugins use.
See the [echo example](https://github.com/luowensheng/wave/tree/main/examples/plugins/echo).

### `longlived` (persistent subprocess, JSON-RPC 2.0)

Wave spawns the process once at boot and keeps it alive. Each call
is a JSON-RPC 2.0 message framed LSP-style.

**Frame format:**

```
Content-Length: <byte length of body>\r\n
\r\n
<JSON body>
```

**Body shape (request):**

```jsonc
{
  "jsonrpc": "2.0",
  "id":      42,                  // uint64; mandatory for requests, omit for notifications
  "method":  "handler.call",      // see method table per kind below
  "params":  { /* method-specific */ }
}
```

**Body shape (response):**

```jsonc
// Success
{ "jsonrpc": "2.0", "id": 42, "result": { /* method-specific */ } }

// Error
{ "jsonrpc": "2.0", "id": 42, "error": { "code": -32603, "message": "...", "data": ... } }
```

Wave drains on SIGINT and closes the plugin's stdin. Implement a
clean shutdown when stdin returns EOF.

### `http` (remote service)

The plugin is a long-running HTTP server. Each call is an HTTP POST
to `<address>/<method>` with the request body as JSON.

| Field | Value |
|---|---|
| URL | `<address>/<method>` where `<method>` is e.g. `handler.call` |
| Method | `POST` |
| Body | JSON `{params}` |
| Response | JSON `{result}` on 2xx, JSON `{code, message, data}` on non-2xx |
| Timeout | per-plugin `timeout:` config (default 5s) |

Use when the plugin is already a service running elsewhere.

## Common request fields

Every transport passes the same per-request envelope. Fields that
don't apply to a given kind are omitted.

| Field | Type | When |
|---|---|---|
| `trigger_key` | string | `handler` kind — `plugin.trigger_key` from the route |
| `metadata` | map[string]string | `handler` — request metadata (`remote_ip`, `request_id`, etc.) |
| `headers` | map[string]string | `handler` — request headers (canonicalized) |
| `cookies` | map[string]string | `handler` — request cookies |
| `query` | map[string]string | `handler` — URL query params |
| `path_params` | map[string]string | `handler` — path variables from the route mux pattern |
| `body` | raw JSON | `handler` — request body, JSON-decoded once |

---

## Method set per kind

### Kind: `handler`

The most common kind. Backs `type: plugin` routes.

| Method | Params | Result | Purpose |
|---|---|---|---|
| `handler.call` | full request envelope (above) | `{status, headers, body}` | Execute the request and return an HTTP response |

`subprocess` plugins skip the JSON-RPC layer entirely — Wave just
calls them with the raw envelope and parses the raw response.

### Kind: `storage`

Backs `storage: { type: plugin }` backends. Each method is
independently invocable from any route that references the storage.

| Method | Params | Result |
|---|---|---|
| `storage.get` | `{key: string}` | `{value: []byte, present: bool}` |
| `storage.set` | `{key: string, value: []byte}` | `{}` |
| `storage.delete` | `{key: string}` | `{}` |
| `storage.query` | `{sql: string, args: []any, params: {string:any}}` | `{columns: [], rows: [{}], last_insert_id, rows_affected}` |
| `storage.migrate` | `{statements: []string}` | `{}` |

Use `storage.query` for arbitrary SQL (driven by the route's
`storage-access.execute:`). `storage.get/set/delete` are the simpler
KV API that `type: kv-store` style routes use.

See [`postgres-storage` reference plugin](https://github.com/luowensheng/wave/tree/main/examples/plugins/postgres-storage).

### Kind: `auth`

Backs `auth: { type: plugin }` providers. Wave calls these from the
auth-login / oauth-callback / magic-link-consume flows.

| Method | Params | Result |
|---|---|---|
| `auth.authenticate` | `{method, credentials, headers, cookies}` | `{authenticated: bool, claims, redirect, set_cookies}` |
| `auth.refresh_claims` | `{subject: string}` | `{subject, email, roles, scopes, raw}` (Claims) |
| `auth.logout` | `{subject: string}` | `{}` |

`AuthRequest.method`: `"password"`, `"magic_link"`, `"oauth"`,
`"saml"`, or whatever your plugin advertises in its manifest.

`AuthResult.claims` becomes the request-context claims for RBAC
(`require_roles`, `require_claims`).

See [`saml-auth` reference plugin](https://github.com/luowensheng/wave/tree/main/examples/plugins/saml-auth).

### Kind: `secrets`

Backs `${PLUGIN:name:uri}` interpolation in `server.yaml`.

| Method | Params | Result |
|---|---|---|
| `secrets.resolve` | `{uri: string}` | `{value: []byte}` |

`uri` is whatever the user put after the colon — e.g.
`${PLUGIN:vault:secret/data/stripe#api_key}` calls `secrets.resolve`
with `uri: "secret/data/stripe#api_key"`. The plugin defines the URI
grammar.

See [`vault-secrets` reference plugin](https://github.com/luowensheng/wave/tree/main/examples/plugins/vault-secrets).

### Kind: `exporter`

Receives observability data via fanout. Wave batches metrics, logs,
and traces and invokes the matching method asynchronously.

| Method | Params | Result |
|---|---|---|
| `exporter.metrics` | `{samples: [MetricSample]}` | `{}` |
| `exporter.traces` | `{spans: [TraceSpan]}` | `{}` |
| `exporter.logs` | `{records: [LogRecord]}` | `{}` |

Errors from exporters are logged and dropped — they don't propagate
to the original request (fire-and-forget).

See [`otel-exporter` reference plugin](https://github.com/luowensheng/wave/tree/main/examples/plugins/otel-exporter).

---

## Type definitions

### `Request` (handler)

```jsonc
{
  "trigger_key": "chat",
  "metadata":    {"remote_ip": "1.2.3.4", "request_id": "..."},
  "headers":     {"authorization": "Bearer ..."},
  "cookies":     {"session": "..."},
  "query":       {"prompt": "hello"},
  "path_params": {"id": "42"},
  "body":        {"any":"json"}
}
```

### `Response` (handler)

```jsonc
{
  "status":  200,
  "headers": {"Content-Type": "application/json"},
  "body":    {"any":"json"}
}
```

### `Query` (storage)

```jsonc
{
  "sql":    "SELECT * FROM users WHERE id = $1",
  "args":   [42],
  "params": {"id": 42}
}
```

`args` is positional; `params` is named. Most plugins use one or the
other based on which the underlying driver prefers.

### `QueryResult` (storage)

```jsonc
{
  "columns":        ["id", "name"],
  "rows":           [{"id": 1, "name": "ada"}],
  "last_insert_id": 1,
  "rows_affected":  1
}
```

### `Claims` (auth)

```jsonc
{
  "subject":        "user@example.com",
  "email":          "user@example.com",
  "email_verified": true,
  "name":           "Ada Lovelace",
  "roles":          ["admin", "developer"],
  "scopes":         ["read:items", "write:items"],
  "provider":       "okta",
  "raw":            {"any": "claim from the IdP"}
}
```

### `MetricSample` (exporter)

```jsonc
{
  "name":   "wave_http_requests_total",
  "type":   "counter",
  "value":  1.0,
  "labels": {"route":"/items","status":"200"},
  "ts":     1700000000
}
```

### `TraceSpan` (exporter)

```jsonc
{
  "trace_id":        "01234...",
  "span_id":         "56789...",
  "parent_span_id":  "abcde...",
  "name":            "GET /items",
  "start_unix_nano": 1700000000000000000,
  "end_unix_nano":   1700000000123000000,
  "attributes":      {"http.method": "GET", "http.status": "200"}
}
```

### `LogRecord` (exporter)

```jsonc
{
  "ts":      1700000000123456789,
  "level":   "info",
  "msg":     "request",
  "fields":  {"method":"GET","path":"/items","status":200}
}
```

---

## Manifest

Plugin authors should ship a `manifest.json` alongside the binary so
`wave doctor` can validate compatibility:

```jsonc
{
  "name":         "echo",
  "kind":         "handler",
  "version":      "0.1.0",
  "description":  "Echoes the request back",
  "author":       "your-org",
  "config_schema": {
    "type":     "object",
    "properties": {
      "API_KEY": {"type": "string", "description": "Required API key"}
    },
    "required": ["API_KEY"]
  }
}
```

`config_schema` is optional. When present, Wave validates the
`config:` block under your plugin in `server.yaml` against this
schema at boot.

---

## Calling a plugin from `server.yaml`

```yaml
plugins:
  myplugin:
    transport: process              # or 'longlived' or 'http'
    kind:      handler              # optional; defaults to 'handler'
    command:   ["/usr/local/bin/wave-myplugin"]  # for process/longlived
    address:   "http://127.0.0.1:9999"           # for http
    timeout:   5s                   # max per-call duration
    retries:   3                    # retry count on transient failure
    retry_backoff: 100ms            # initial backoff (exponential)
    env:                            # env vars for the subprocess
      API_KEY: "${env:MY_API_KEY}"
    config:                         # opaque per-plugin config (validated against manifest.config_schema)
      key1: value1

# As a route handler
routes:
  - path: /api/x
    method: POST
    type: plugin
    plugin:
      name:        myplugin
      trigger_key: my_action

# As a storage backend
storage:
  mybackend:
    type:   plugin
    plugin: myplugin
    config: { dsn: "${env:DSN}" }

# As an auth provider
auth:
  custom:
    type:   plugin
    plugin: myplugin

# As a secrets resolver: ${PLUGIN:myplugin:some/uri/string}
```

---

## Retries and timeouts

| Setting | Default | Behavior |
|---|---|---|
| `timeout` | 5s | Kill the call (subprocess: kill process; http: abort request) |
| `retries` | 0 | Retry count on transient failure (network error, non-2xx for http) |
| `retry_backoff` | 50ms | Initial backoff; exponential with jitter, capped at 2s |

Retries are NOT applied to:
- `handler.call` results where the plugin returned a clean `{error}`
  — that's an intentional outcome, not a transport failure
- `exporter.*` methods — fire-and-forget anyway

## See also

- [Build a plugin (in any language)](/cookbook/build-plugin) — worked examples
- [Plugins concept](/guide/concepts-plugins) — the high-level picture
- [`sdk/wave/`](https://github.com/luowensheng/wave/tree/main/sdk/wave) — Go SDK source (canonical reference implementation)
- [`docs/plugins.md`](https://github.com/luowensheng/wave/blob/main/docs/plugins.md), [`auth-plugins.md`](https://github.com/luowensheng/wave/blob/main/docs/auth-plugins.md), [`storage-plugins.md`](https://github.com/luowensheng/wave/blob/main/docs/storage-plugins.md), [`secrets-plugins.md`](https://github.com/luowensheng/wave/blob/main/docs/secrets-plugins.md), [`exporter-plugins.md`](https://github.com/luowensheng/wave/blob/main/docs/exporter-plugins.md) — per-kind detail docs
- 6 reference implementations in [`examples/plugins/`](https://github.com/luowensheng/wave/tree/main/examples/plugins)
