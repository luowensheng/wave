# Build a plugin (in any language)

Wave plugins are **out-of-process binaries** that speak a tiny JSON
contract. That means:

- A plugin can be written in **any language** that can read JSON
  from stdin and write JSON to stdout — Go, Python, Node, Rust,
  Ruby, Bash, even a one-line shell pipeline.
- A misbehaving plugin **can't crash Wave** — it runs in a separate
  process.
- You can **ship plugin binaries independently** from your `wave`
  binary. Different release cadence, different team, different
  language.

This page walks through the same minimal "echo" plugin in five
languages, then wires it into `server.yaml`.

::: tip If you're writing Go
The official [`wave.dev/sdk/wave`](https://github.com/luowensheng/wave/tree/main/sdk/wave)
package handles the JSON-RPC framing for long-lived plugins.
For one-shot subprocess plugins (the simplest case), the contract
is small enough that you may not need an SDK.
:::

## The two transports

Pick one based on how stateful your work is:

| Transport | Process lifecycle | Best for |
|---|---|---|
| `process` | Spawn per request, JSON in/out, exit | Stateless work — shell wrappers, simple transformations, calling other tools |
| `longlived` | Spawn once, JSON-RPC over stdin/stdout per call | Stateful — DB connection pools, loaded ML models, anything with expensive init |
| `http` | Long-running HTTP server you operate elsewhere | When the plugin is already a service |

Start with `process` — it's the simplest. Move to `longlived` when
per-request spawn overhead matters.

## The contract (subprocess, one-shot)

Each request, Wave runs your binary and feeds it JSON on stdin:

```jsonc
{
  "trigger_key": "hello",                       // route's plugin.trigger_key
  "metadata":    {"remote_ip": "1.2.3.4", "request_id": "abc"},
  "headers":     {"x-tenant": "acme"},
  "cookies":     {"session": "..."},
  "query":       {"name": "ada"},
  "path_params": {"id": "42"},                  // path variables from the route
  "body":        {"raw":"json","payload":"here"}  // request body, JSON-decoded
}
```

You write JSON on stdout, exit 0:

```jsonc
{
  "status":  200,
  "headers": {"Content-Type": "application/json"},
  "body":    {"any":"json","shape":"you","want":true}
}
```

That's it. Five fields in, three out. No framing, no IDs.

---

## Worked example: same plugin in five languages

We'll build a plugin that:
- Reads the JSON request from stdin
- Echoes the body back with `{"trigger_key": ..., "echo": ..., "remote_ip": ...}`
- Sets `X-Echo-Plugin: 1` header
- Returns 200

### Go

::: code-group

```go [echo.go (no SDK)]
package main

import (
  "encoding/json"
  "os"
)

type request struct {
  TriggerKey string            `json:"trigger_key"`
  Metadata   map[string]string `json:"metadata"`
  Body       json.RawMessage   `json:"body"`
}

type response struct {
  Status  int               `json:"status"`
  Headers map[string]string `json:"headers,omitempty"`
  Body    json.RawMessage   `json:"body,omitempty"`
}

func main() {
  var req request
  if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
    os.Exit(1)
  }
  body, _ := json.Marshal(map[string]any{
    "trigger_key": req.TriggerKey,
    "echo":        json.RawMessage(req.Body),
    "remote_ip":   req.Metadata["remote_ip"],
  })
  _ = json.NewEncoder(os.Stdout).Encode(response{
    Status:  200,
    Headers: map[string]string{"X-Echo-Plugin": "1"},
    Body:    body,
  })
}
```

```go [echo.go (with SDK — long-lived)]
package main

import (
  "context"
  "encoding/json"
  "os"

  sdk "github.com/luowensheng/wave/sdk/wave"
)

type echo struct{}

func (echo) Call(_ context.Context, req *sdk.Request) (*sdk.Response, error) {
  body, _ := json.Marshal(map[string]any{
    "trigger_key": req.TriggerKey,
    "echo":        json.RawMessage(req.Body),
    "remote_ip":   req.Metadata["remote_ip"],
  })
  return &sdk.Response{
    Status:  200,
    Headers: map[string]string{"X-Echo-Plugin": "1"},
    Body:    body,
  }, nil
}

func (echo) Close() error { return nil }

func main() {
  if err := sdk.RunHandler(echo{}); err != nil {
    os.Exit(1)
  }
}
```

:::

Build:

```sh
go build -o /usr/local/bin/wave-echo ./echo.go
```

### Python

```python
#!/usr/bin/env python3
"""echo plugin — read JSON request from stdin, write JSON response to stdout."""
import json, sys

req = json.load(sys.stdin)

resp = {
    "status":  200,
    "headers": {"X-Echo-Plugin": "1"},
    "body": {
        "trigger_key": req.get("trigger_key"),
        "echo":        req.get("body"),
        "remote_ip":   (req.get("metadata") or {}).get("remote_ip"),
    },
}
json.dump(resp, sys.stdout)
```

```sh
chmod +x echo.py
mv echo.py /usr/local/bin/wave-echo
```

### Node.js

```js
#!/usr/bin/env node
// echo plugin — read JSON request from stdin, write JSON response to stdout.
const data = require('fs').readFileSync(0, 'utf-8')   // 0 = stdin
const req  = JSON.parse(data)

const resp = {
  status:  200,
  headers: { 'X-Echo-Plugin': '1' },
  body: {
    trigger_key: req.trigger_key,
    echo:        req.body,
    remote_ip:   req.metadata?.remote_ip,
  },
}
process.stdout.write(JSON.stringify(resp))
```

```sh
chmod +x echo.js
mv echo.js /usr/local/bin/wave-echo
```

### Rust

```rust
// echo plugin — Cargo.toml needs `serde_json = "1"`
use std::io::{self, Read, Write};
use serde_json::{Value, json};

fn main() -> io::Result<()> {
    let mut input = String::new();
    io::stdin().read_to_string(&mut input)?;
    let req: Value = serde_json::from_str(&input).unwrap();

    let resp = json!({
        "status":  200,
        "headers": {"X-Echo-Plugin": "1"},
        "body": {
            "trigger_key": req["trigger_key"],
            "echo":        req["body"],
            "remote_ip":   req["metadata"]["remote_ip"],
        }
    });
    io::stdout().write_all(serde_json::to_vec(&resp)?.as_slice())
}
```

```sh
cargo build --release
mv target/release/echo /usr/local/bin/wave-echo
```

### Bash (with `jq`)

For when you want to wrap an existing CLI tool:

```bash
#!/usr/bin/env bash
# echo plugin — pure shell + jq
set -euo pipefail

jq -c '{
  status: 200,
  headers: { "X-Echo-Plugin": "1" },
  body: {
    trigger_key: .trigger_key,
    echo:        .body,
    remote_ip:   .metadata.remote_ip
  }
}'
```

```sh
chmod +x echo.sh
mv echo.sh /usr/local/bin/wave-echo
```

That's a complete Wave plugin in **9 lines of shell**.

---

## Wire it into `server.yaml`

Same config regardless of which language you used:

```yaml
plugins:
  echo:
    transport: process              # one-shot per request
    command: ["/usr/local/bin/wave-echo"]
    timeout: 5s                     # kill if it hangs

routes:
  - path: /echo
    method: POST
    type: plugin
    plugin:
      name: echo
      trigger_key: hello            # arbitrary string, sent in the request
```

Run it:

```sh
wave serve server.yaml --port 8080
curl -X POST -d '{"any":"json","payload":42}' http://localhost:8080/echo
# {"trigger_key":"hello","echo":{"any":"json","payload":42},"remote_ip":"127.0.0.1"}
```

---

## When per-request spawn is too slow: `longlived`

The one-shot model spawns a new process per request — fine for ms-level
work, painful for ML-style work that loads gigabytes into memory.
`longlived` keeps your process alive between requests and frames each
call as a JSON-RPC 2.0 message.

```yaml
plugins:
  model:
    transport: longlived            # process held open
    kind: handler
    command: ["python3", "/opt/wave/model_server.py"]
    timeout: 30s
```

Wire format: each frame is `Content-Length: N\r\n\r\n<json>` (LSP-style).
Method names match the [plugin contract reference](/reference/plugin-contract).

The easiest path is to use the SDK:

::: code-group

```go [Go SDK]
package main

import (
  "context"
  sdk "github.com/luowensheng/wave/sdk/wave"
)

type model struct {
  // expensive state loaded once at boot
  weights *MLWeights
}

func (m *model) Call(ctx context.Context, req *sdk.Request) (*sdk.Response, error) {
  out := m.weights.Predict(req.Body)
  return &sdk.Response{Status: 200, Body: out}, nil
}

func (m *model) Close() error { return m.weights.Close() }

func main() {
  m := &model{weights: LoadOnce("/opt/model.bin")}
  sdk.RunHandler(m)   // blocks; handles the JSON-RPC framing for you
}
```

```python [Python]
# Roll your own with the official wire spec; or use the
# minimal `wave-plugin-py` template at:
# github.com/luowensheng/wave-plugin-template
import sys, json, struct

def read_frame():
    headers = b''
    while not headers.endswith(b'\r\n\r\n'):
        b = sys.stdin.buffer.read(1)
        if not b: return None
        headers += b
    length = int(headers.split(b'Content-Length:')[1].strip().split(b'\r\n')[0])
    return json.loads(sys.stdin.buffer.read(length))

def write_frame(obj):
    body = json.dumps(obj).encode()
    sys.stdout.buffer.write(b'Content-Length: %d\r\n\r\n%s' % (len(body), body))
    sys.stdout.buffer.flush()

# Load model ONCE here, before the request loop.
model = load_model()

while True:
    req = read_frame()
    if req is None: break  # wave closed stdin
    body = json.loads(req['params']['body'] or b'null')
    out  = model.predict(body)
    write_frame({
        'jsonrpc': '2.0',
        'id':       req['id'],
        'result':   {'status': 200, 'body': out},
    })
```

:::

The full JSON-RPC method set per plugin kind is in the
[plugin contract reference](/reference/plugin-contract).

---

## Plugin kinds

The same transport, different responsibilities. Set `kind:` in your
plugin config (or in the manifest if you ship the plugin separately):

| `kind:` | Wave calls it for | Example |
|---|---|---|
| `handler` *(default)* | `type: plugin` routes | echo, image-resize, slack-notifier |
| `storage` | `storage: { type: plugin }` backends | postgres, redis, dynamodb |
| `auth` | `auth: { type: plugin }` providers | LDAP, custom IdP, SAML |
| `secrets` | `${PLUGIN:name:uri}` interpolation | HashiCorp Vault, AWS Secrets Manager |
| `exporter` | Observability fanout (metrics/logs/traces) | OTel collector bridge, Datadog, custom SaaS |

Each kind has its own method set. See the [plugin contract
reference](/reference/plugin-contract) for the exact JSON-RPC
methods to implement per kind.

---

## Reference implementations to copy from

All under [`examples/plugins/`](https://github.com/luowensheng/wave/tree/main/examples/plugins):

| Plugin | Kind | Language | What it does |
|---|---|---|---|
| [`echo`](https://github.com/luowensheng/wave/tree/main/examples/plugins/echo) | handler | Go (no SDK) | Subprocess one-shot reference — 40-line `main.go` |
| [`echo-handler`](https://github.com/luowensheng/wave/tree/main/examples/plugins/echo-handler) | handler | Go (SDK) | Long-lived reference using `sdk.RunHandler` |
| [`postgres-storage`](https://github.com/luowensheng/wave/tree/main/examples/plugins/postgres-storage) | storage | Go (SDK) | PostgreSQL backend via pgx |
| [`vault-secrets`](https://github.com/luowensheng/wave/tree/main/examples/plugins/vault-secrets) | secrets | Go (SDK) | Resolve `${PLUGIN:vault:secret/path}` against Vault KV-v2 |
| [`saml-auth`](https://github.com/luowensheng/wave/tree/main/examples/plugins/saml-auth) | auth | Go (SDK) | SAML 2.0 SP via crewjam/saml |
| [`otel-exporter`](https://github.com/luowensheng/wave/tree/main/examples/plugins/otel-exporter) | exporter | Go (SDK) | OpenTelemetry OTLP exporter for metrics + traces |

And nine demo apps that use real plugins end-to-end:

| Demo | Plugins used |
|---|---|
| [`multi-plugin-stack`](https://github.com/luowensheng/wave/tree/main/examples/apps/multi-plugin-stack) | Multiple plugins in one server.yaml |
| [`postgres-crud`](https://github.com/luowensheng/wave/tree/main/examples/apps/postgres-crud) | `postgres-storage` |
| [`postgres-plugin-crud`](https://github.com/luowensheng/wave/tree/main/examples/apps/postgres-plugin-crud) | `postgres-storage` |
| [`vault-secrets-fed`](https://github.com/luowensheng/wave/tree/main/examples/apps/vault-secrets-fed) | `vault-secrets` |
| [`saml-sso`](https://github.com/luowensheng/wave/tree/main/examples/apps/saml-sso) / [`saml-enterprise-sso`](https://github.com/luowensheng/wave/tree/main/examples/apps/saml-enterprise-sso) | `saml-auth` |
| [`otel-tracing-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/otel-tracing-demo) | `otel-exporter` |
| [`background-task-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/background-task-demo) | A `handler` plugin emitting ndjson |
| [`scheduled-jobs-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/scheduled-jobs-demo) | Plugin called from a cron action |

---

## Production checklist for shipping a plugin

- [ ] **Bake a `manifest.json`** with `name`, `kind`, `version`,
      `description`. Wave reads it for `wave doctor` output.
- [ ] **Validate inputs** — even though Wave validates `inputs:` on
      the route, your plugin can still receive a malformed body if a
      caller bypasses the route. Treat the body as untrusted.
- [ ] **Set `timeout:`** in the plugin config. Wave kills the
      process if it hangs.
- [ ] **Exit non-zero on fatal errors** — Wave reports the exit
      code in `wave_plugin_call_duration_seconds` labels.
- [ ] **Log to stderr** — Wave's stdout reader assumes JSON.
      Anything you print to stdout breaks the response.
- [ ] **For `longlived`**: handle a clean shutdown when stdin closes
      (Wave drains on SIGINT).
- [ ] **For `process`**: keep startup fast. Each request pays the
      spawn cost.
- [ ] **Reproducible builds** with pinned dependencies. Plugin
      binaries should be content-addressable.
- [ ] **Distribute as a binary** in a tarball or a Docker image
      sidecar. Don't require users to install your build chain.

## See also

- [Plugins concept](/guide/concepts-plugins) — the bigger picture
- [Plugin contract reference](/reference/plugin-contract) — every
  JSON-RPC method per kind, full wire format
- [`docs/plugins.md`](https://github.com/luowensheng/wave/blob/main/docs/plugins.md)
  — formal contract spec
- [`sdk/wave/`](https://github.com/luowensheng/wave/tree/main/sdk/wave)
  — Go SDK source (use as a reference if porting to another language)
