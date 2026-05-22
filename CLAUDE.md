# Wave — Claude Development Guide

Wave is a YAML-driven Go HTTP server framework. A single `server.yaml` file declares
storage, plugins, connections, routes, schedule, and auth — no hand-written Go required
for app code. This document tells Claude how to work inside this codebase correctly.

---

## Project layout

```
wave/                         ← module name (go.mod)
  infra/                      ← pure infrastructure, no business logic
    sqlite/                   ← SQLite storage backend
    inputs/                   ← request input parsing, validation, context
    connections/              ← SSE/WebSocket broker registry
    plugins/                  ← plugin client registry and transports
    render/                   ← Go template renderer (output_template)
    cron/                     ← in-process scheduler
  io/http/contentloader/      ← DataLoader / ContentLoader abstractions
  usecases/                   ← route-type business logic
    storage_access/           ← type: storage-access
    task/                     ← type: task
    routes/                   ← type aliases exported for orchestrator
  orchestrator/server/        ← server boot, route wiring, scheduler
  examples/apps/              ← demo server.yaml files (one dir each)
```

Go version: **1.24.1**. Module name: **`wave`**.

---

## Code style

- Standard Go idioms. No frameworks, no generics unless genuinely needed.
- Exported symbols need doc comments. Unexported helpers don't have to.
- `any` over `interface{}` everywhere.
- Error strings are lowercase, no trailing punctuation.
- Prefer early returns over deep nesting.
- Never `log.Fatal` inside a usecase package — return errors, let the orchestrator handle them.
- Struct tags: `yaml:"..."` and `json:"..."` on every field that goes through YAML/JSON.
  Use `omitempty` unless the zero value is meaningful.
- No `init()` functions — use explicit wiring functions called from `InitDependencies`.

---

## Adding a new route type

Follow this checklist exactly — skipping steps causes silent misses at boot.

1. **Create `usecases/<name>/config.go`**
   - Define `Config` struct with YAML tags.
   - Implement `CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error)`.
   - Validate config at `CreateRoute` time so failures surface at boot, not at request time.
   - Global injected dependencies go in package-level `var` func pointers (see `GetStorageFn` pattern).

2. **Create `usecases/routes/<name>_config.go`**
   ```go
   package routes
   import "wave/usecases/<name>"
   type <Name>Config = <name>.Config
   ```

3. **Add field to `orchestrator/server/route.go` `Route` struct**
   ```go
   <Name>Config *routes.<Name>Config `yaml:"<name>,omitempty" json:"<name>,omitempty"`
   ```

4. **Add case in `route.go` `getRouteConfig()`**
   ```go
   case "<name>":
       routeConfig = route.<Name>Config
   ```

5. **Wire injected dependencies in `orchestrator/server/servers.go` `InitDependencies()`**
   after the relevant subsystem is initialized.

6. **Write tests** in `usecases/<name>/config_test.go`.

---

## The `inputs:` block (route-level)

Declare every value a route accepts. The middleware extracts, coerces, and validates them
before the handler runs. The result is available via `inputs.FromContext(r.Context())`.

```yaml
inputs:
  - name: user_id      # template key name
    source: path       # path | query | body | form | header | cookie | body_raw
    type: int          # string | int | float | bool | email | uuid | file | bytes | array | object
    required: true
    min: 1             # numeric: minimum value; string: minimum length
    max: 9999          # numeric: maximum value; string: maximum length
    pattern: "^[a-z]+" # regexp, strings only
    enum: [a, b, c]    # allowed values, strings only
    default: 0         # used when the field is absent and not required
```

**Sources:**
| source | reads from |
|---|---|
| `path` | URL path segment `{name}` |
| `query` | `?name=value` |
| `body` | named field of JSON / form / multipart body |
| `form` | alias for `body` when Content-Type is form/multipart |
| `header` | request header |
| `cookie` | cookie value |
| `body_raw` | the entire raw body as `[]byte` (requires `type: bytes`) |

**Types:**
- `file` — multipart file upload; value is `*inputs.File`. Requires `source: body` or `source: form` and `expected_content_type: multipart/form-data` on the route.
- `bytes` — raw body bytes; requires `source: body_raw`.
- `array` — JSON array; passed through as `[]any`.
- `object` — JSON object; passed through as `map[string]any`.

**`expected_content_type`** — set on the route when the body isn't JSON:
```yaml
expected_content_type: multipart/form-data   # file uploads
expected_content_type: application/x-www-form-urlencoded
expected_content_type: text/plain            # with type: bytes, source: body_raw
```

---

## SQL rules — read this carefully

### The golden rule

**Never put raw values into SQL `execute` fields.** Every value that reaches SQL must
go through the `{{name}}` → `?` parameterised binding path. Dot-notation (`{{.name}}`),
`bind`, `wrap`, or any other template expression that produces a literal SQL value
is only acceptable when the source of that value is already safe (e.g. `{{getCurrentTime}}`
which inserts a server-computed timestamp).

### The `{{name}}` syntax

Inside any `execute:` string, each `{{name}}` is a **function call** registered in the
SQLite Execute template `funcMap`. It appends the value to the params slice and emits `?`.
This is how parameterised binding works — the template produces the SQL skeleton and the
Go driver binds values separately.

```yaml
execute: "SELECT * FROM users WHERE id = {{user_id}} AND active = {{active}}"
```

The value of `user_id` and `active` must be declared in the route's `inputs:` block (or
in the step's `inputs:` map for pipeline routes). If a name is referenced in SQL but not
declared, the DataLoader returns an error and the request aborts with 500.

### What works in execute strings

```yaml
# Good — declared input
execute: "INSERT INTO t (col) VALUES ({{value}})"

# Good — server-generated timestamp
execute: "INSERT INTO t (ts) VALUES ({{getCurrentTime}})"
execute: "INSERT INTO t (ts) VALUES ({{getCurrentTimeLocal}})"
execute: "INSERT INTO t (expires) VALUES ({{addDays 7}})"

# Good — formatted timestamp
execute: "INSERT INTO t (ts) VALUES ({{formatTime \"2006-01-02\"}})"

# Good — conditional presence check
execute: >
  SELECT * FROM t
  WHERE 1=1
  {{if hasvalue "filter"}} AND col = {{filter}} {{end}}

# Good — JSON array for json_each
execute: >
  SELECT * FROM items
  WHERE tag IN (SELECT value FROM json_each({{jsonArray (raw "tags")}}))

# Good — wrap with prefix/suffix (e.g. LIKE %value%)
execute: "SELECT * FROM t WHERE name LIKE {{wrap \"%name%\"}}"

# Good — multi-statement: preamble + final SELECT
execute: |
  UPDATE pastes SET views = views + 1 WHERE slug = {{slug}};
  SELECT * FROM pastes WHERE slug = {{slug}} LIMIT 1
```

```yaml
# BAD — dot-notation bypasses parameterisation
execute: "INSERT INTO t (col) VALUES ({{.user_id}})"      # NEVER

# BAD — bind with dot-navigation
execute: "INSERT INTO t (col) VALUES ({{bind .user.id}})" # NEVER

# BAD — toJSON in SQL
execute: "INSERT INTO t (j) VALUES ({{toJSON .data}})"    # NEVER

# BAD — any raw Go template expression producing a literal
execute: "SELECT * FROM t WHERE x = {{printf \"%s\" .x}}" # NEVER
```

### Multi-statement SQL

When `execute` contains multiple `;`-separated statements:
- All but the last run with `db.Exec` (no result captured).
- The last statement drives the response (`SELECT` → rows, `INSERT` → `LastInsertID`, etc.).
- Parameters are distributed by counting `?` placeholders per statement (via `splitStatements` + `countParams` in `infra/sqlite/sqlite_utils.go`).
- The query type (SELECT / INSERT / UPDATE / DELETE) is classified from the **last** statement.

```yaml
# Increment view counter then fetch the row
execute: |
  UPDATE pastes SET views = views + 1 WHERE slug = {{slug}};
  SELECT slug, content, views FROM pastes WHERE slug = {{slug}} LIMIT 1
```

### Single-row vs multi-row result shape

`isSingleRowQuery` detects these patterns and returns `map[string]any` instead of `[]map[string]any`:
- `LIMIT 1` present in the outermost query.
- Aggregate (`COUNT`, `SUM`, `AVG`, `MIN`, `MAX`) without `GROUP BY`.
- Scalar subquery (detected heuristically).

This matters for `output_template` and pipeline dot-paths:
- Single-row result → `.Data.column` (map field access)
- Multi-row result → `.Data` is `[]map[string]any` — use `{{toJSON .Data}}` for JSON

```yaml
# Single-row (LIMIT 1) — access fields directly
execute: "SELECT id, name FROM users WHERE id = {{user_id}} LIMIT 1"
output_template: '{"id":{{.Data.id}},"name":"{{.Data.name}}"}'

# Multi-row — .Data is a slice, use toJSON
execute: "SELECT id, name FROM users"
output_template: '{{toJSON .Data}}'
```

### `if_empty_status`

For GET routes that should 404 when no row is found:
```yaml
storage-access:
  execute: "SELECT * FROM items WHERE id = {{id}} LIMIT 1"
  if_empty_status: 404
  output_template: '{{toJSON .Data}}'
```
Returns `{"error":"not found"}` with status 404 when `Data` is nil, empty slice, or empty map.

### Available SQL template helpers

| helper | produces | notes |
|---|---|---|
| `{{name}}` | `?` | primary — binds declared input |
| `{{value "name"}}` | `?` | alias for the above |
| `{{getCurrentTime}}` | `?` | UTC ISO-8601 timestamp |
| `{{getCurrentTimeLocal}}` | `?` | local timezone ISO-8601 |
| `{{addDays N}}` | `?` | now + N days UTC |
| `{{formatTime "layout"}}` | `?` | Go time format string |
| `{{wrap "pattern"}}` | `?` | value with prefix/suffix (e.g. `%name%`) |
| `{{bind val}}` | `?` | binds an in-template expression safely |
| `{{raw "name"}}` | Go value | raw declared-input value (no binding) — feed to `jsonArray` |
| `{{jsonArray (raw "name")}}` | `'[...]'` | SQL JSON literal for `json_each` |
| `{{hasvalue "name"}}` | bool | true if input is present and non-empty |
| `{{hasvalues "a" "b"}}` | bool | AND of hasvalue checks |
| `{{hasanyvalue "a" "b"}}` | bool | OR of hasvalue checks |
| `{{iterlist "name"}}` | `[]string` | iterate a declared `type: array` input |
| `{{itermap "name"}}` | `[]string` | iterate a declared `type: object` input |
| `{{getindex "name" N}}` | `?` | binds `name[N]` from an array input |
| `{{getUser}}` | `*PublicUser` | authenticated user from session |
| `{{error "msg"}}` | aborts | render-time abort with error |

---

## `type: storage-access`

### Single-step

```yaml
- path: /items/{id}
  method: GET
  type: storage-access
  inputs:
    - { name: id, source: path, type: int, required: true }
  storage-access:
    source: my_db              # storage name from top-level `storage:`
    execute: "SELECT * FROM items WHERE id = {{id}} LIMIT 1"
    if_empty_status: 404
    response_content_type: application/json
    output_template: '{{toJSON .Data}}'
```

`output_template` context when `inputs:` are declared: a merged `map[string]any` with all
exported `ExecuteResult` fields (`.Data`, `.LastInsertID`, `.RowsAffected`, `.ColumnNames`)
plus all declared input values overlaid. Both `{{.Data}}` and `{{Data}}` work.

### Pipeline

Chain multiple queries. Each step's `inputs:` map declares exactly which values its SQL
may use and where to source them from the accumulated map.

```yaml
storage-access:
  steps:
    - source: my_db
      inputs:
        user_id: ""          # "" → key name in accumulator (request input)
      execute: "SELECT id, name FROM users WHERE id = {{user_id}} LIMIT 1"
      as: user

    - source: my_db
      inputs:
        uid: user.id         # dot-path: accum["user"]["id"]
      execute: "SELECT * FROM orders WHERE user_id = {{uid}} ORDER BY id"
      as: orders

  response_content_type: application/json
  output_template: '{"user":{{toJSON .user}},"orders":{{toJSON .orders}}}'
```

**Dot-path rules for `inputs:` values:**
- `""` — same as the key name (simple accumulator lookup)
- `"."` — the entire accumulator (JSON-encoded when bound as SQL param)
- `"user.id"` — `accum["user"]` as map, then `["id"]`
- `"orders.0.item"` — `accum["orders"]` as slice, index 0, then `["item"]`

The accumulator is seeded with all declared request inputs, then each step's result is
stored under `step.as`. A step result is the `.Data` field of `ExecuteResult` —
single-row `map[string]any` or multi-row `[]map[string]any`.

**Non-scalar values (maps, slices) are automatically JSON-encoded to string** by `ToSQLParam`
before being placed in the step's DataLoader — the SQLite driver can bind them as TEXT.

### `response_content_type: $filetype`

Special sentinel — used when the storage backend returns a `*contentloader.File`.
Sets `Content-Type` from the file extension and streams the binary content directly.
Do not set `output_template` when using `$filetype`.

---

## `type: task` (background route)

Returns `202 Accepted + {"task_id":"<hex>"}` immediately. Runs the plugin call in a
detached goroutine. Emits results to an SSE broker and optionally writes to storage.

```yaml
- path: /api/process
  method: POST
  type: task
  inputs:
    - { name: prompt, source: body, type: string, required: true }
  task:
    plugin: my_llm             # plugin name from top-level `plugins:`
    trigger_key: chat
    streaming: false           # true → read response body as ndjson lines
    connection: events         # SSE broker from top-level `connections:` (required)
    event_type: result         # SSE event: label
    store:                     # optional persistence per emitted event
      source: my_db
      inputs:
        content: content       # dot-path into the emitted JSON payload
      execute: "INSERT INTO results (content) VALUES ({{content}})"
```

- `connection` is **required** — every task must publish somewhere.
- `streaming: true` — each non-empty line from `resp.Body` is emitted separately.
- `store.inputs` uses the same dot-path map as pipeline inputs, resolved against the
  emitted payload parsed as `map[string]any`. Non-JSON payloads are wrapped as `{"data":"..."}`.
- SQL in `store.execute` must use `{{name}}` placeholders only — same rules as everywhere.

---

## `type: schedule` (enhanced scheduler)

The map key is the job name — consistent with `plugins`, `connections`, `storage`, `auth`.
This also enables future stop/pause APIs where a job is looked up by name.

```yaml
schedule:
  # Simple plugin call (legacy form — still works)
  old_job:
    every: 30s
    plugin: my_plugin
    trigger_key: run
    body: { mode: full }

  # action + then form
  poll_prices:
    every: 5s
    action:
      type: api              # api | plugin | storage
      url: "https://api.example.com/prices"
      method: GET
      headers:
        Authorization: "Bearer token"
    then:
      - type: storage
        source: my_db
        inputs:
          price: price       # dot-path into the action result map
        execute: "INSERT INTO prices (price) VALUES ({{price}})"
      - type: publish
        connection: market_feed
        event_type: price_update
      - type: plugin
        plugin: notifier
        trigger_key: new_price
```

**Action types:**
- `api` — HTTP request (`method` defaults to GET). Response body JSON-decoded into `map[string]any`. Non-JSON → `{"body":"...","status":N}`.
- `plugin` — calls a registered plugin. Response body JSON-decoded.
- `storage` — executes SQL. Result wrapped as `{"data": <extractedData>}`.

**Sink types (`then:`):**
- `storage` — `inputs:` dot-path map into the action result → strict DataLoader → SQL.
- `publish` — JSON-marshals the result and publishes to the named SSE broker.
- `plugin` — JSON-marshals the result as request body and calls the plugin.

**Same dot-path + parameterised SQL rules apply in sink `inputs:` maps.**

---

## Output templates (`output_template`)

Rendered by `render.Render` using Go's `text/template`. Template data is always
`map[string]any` when inputs are declared (keys are both dot-accessible `.Name` and
bare functions `Name`).

**Available template helpers in output_template:**
- `{{toJSON .Data}}` — JSON-encode any value
- `{{.Data.field}}` — access struct/map field
- `{{.LastInsertID}}` — from INSERT result
- `{{.RowsAffected}}` — from UPDATE/DELETE result
- `{{.slug}}` — any declared input name available directly

`output_template` is **not** SQL — it does not parameterise. It renders the HTTP response
body directly. It is safe to use `{{toJSON .Data}}` here. It is **not** safe to use
user inputs directly in SQL via the output_template path (that's not even possible, but
don't conflate the two template systems).

---

## The DataLoader / strict-scope pattern

This is the security boundary between request values and SQL.

`InputsContentLoader.GetValue(name)` returns an error for any `name` that wasn't
explicitly inserted into the map. This means a SQL template that references `{{undeclared}}`
gets a render error and the request returns 500 — it never reaches the database.

**When building a DataLoader manually** (task store, scheduler sink, pipeline step):
```go
stepVals := map[string]any{
    "user_id": storageaccess.ToSQLParam(resolvedValue),
}
dl := contentloader.NewDataLoaderFromContentLoader(
    fakeReq,                            // minimal *http.Request
    contentloader.NewInputsLoader(stepVals),
)
result, err := storage.Execute(sqlTemplate, dl)
```

Never pass a map directly to `Execute`. Always go through `NewInputsLoader`.

**`ToSQLParam`** must be called on any value before inserting it into the DataLoader:
- Scalars (`string`, `int*`, `float*`, `bool`, `nil`, `[]byte`) pass through unchanged.
- Maps and slices are JSON-encoded to `string` — the SQLite driver can't bind them.

**`ResolvePath(root map[string]any, path string)`** navigates a nested result:
- `"key"` → `root["key"]`
- `"a.b"` → `root["a"].(map)["b"]`
- `"a.0"` → `root["a"].(slice)[0]`

---

## Connections (SSE / WebSocket)

```yaml
connections:
  events:
    type: sse
    subscribe_path: /events/stream   # auto-registered GET route
    buffer_size: 256                 # ring buffer for reconnect replay
```

Publishing from Go:
```go
reg := connections.Default()
if broker, ok := reg.Get("events"); ok {
    broker.Publish([]byte("event: update\ndata: {\"x\":1}\n\n"))
}
```

SSE event wire format:
```
event: <type>\n
data: <payload>\n
\n
```

The SSE route is auto-registered — do **not** add a manual route for `subscribe_path`.

---

## Plugins

```yaml
plugins:
  my_plugin:
    kind: subprocess           # subprocess | http | longlived
    command: ["python3", "worker.py"]

  remote_api:
    kind: http
    url: "http://localhost:9000"
```

The plugin contract: `Request{TriggerKey, Metadata, Body json.RawMessage}` →
`Response{Status, Headers, Body json.RawMessage}`. All transports speak this shape.

Calling from Go:
```go
client, ok := plugins.Default().Get("my_plugin")
resp, err := client.Call(ctx, &plugins.Request{
    TriggerKey: "run",
    Body:       bodyJSON,
})
```

---

## Testing patterns

### Route handler tests

```go
func TestMyRoute(t *testing.T) {
    cfg := &Config{Plugin: "echo", Connection: "events"}
    handler, err := cfg.CreateRoute("POST", "/test", nil)
    if err != nil {
        t.Fatal(err)
    }
    req := httptest.NewRequest("POST", "/test", strings.NewReader(`{"x":1}`))
    rr := httptest.NewRecorder()
    handler(rr, req)
    // assert on rr.Code, rr.Body
}
```

### Plugin injection for tests

```go
reg := plugins.NewRegistry(nil) // empty
plugins.InjectForTest(reg, "my_plugin", &myStubClient{})
plugins.SetDefault(reg)
```

### Connection injection for tests

```go
cfg := map[string]*connections.ConnectionConfig{
    "events": {Type: "sse", SubscribePath: "/events", BufferSize: 8},
}
reg, _ := connections.NewRegistry(cfg)
connections.SetDefault(reg)
broker, _ := reg.Get("events")
ch, unsub, _ := broker.Subscribe("test-client")
defer unsub()
// trigger action, then read from ch
```

### Storage_access tests

Use the exported `ResolvePath` and `ToSQLParam` directly — no HTTP involved:
```go
val, err := storage_access.ResolvePath(root, "user.id")
safe := storage_access.ToSQLParam(val)
```

---

## Do's and don'ts

### Do

- Declare all route inputs in `inputs:` — even if you don't strictly need validation,
  it enables strict-scope SQL and future OpenAPI docs.
- Use `LIMIT 1` on lookups — it signals single-row shape to `isSingleRowQuery` so
  `.Data.field` works in templates instead of `(index .Data 0).field`.
- Use `if_empty_status: 404` on GET-by-id routes instead of rendering an empty template.
- Wire dependencies in `InitDependencies` and validate them at `CreateRoute` time — fail
  fast at boot, not at request time.
- Export helpers that other packages need (`ResolvePath`, `ToSQLParam`, `ExtractResultData`).
- Write a test file alongside every new usecase package.

### Don't

- **Don't use dot-notation in SQL execute strings.** `{{.name}}` doesn't bind a SQL
  parameter — it renders the Go value as a string literal, which is SQL injection.
- **Don't skip the inputs DataLoader.** Passing raw `map[string]any` to `Execute`
  through any path other than `NewInputsLoader` bypasses the strict-scope check.
- **Don't declare `init()` functions.** Wire everything explicitly.
- **Don't use `log.Fatal` in usecase packages.** Return errors upward.
- **Don't create new top-level YAML keys** without updating `Config` in `servers.go`
  and adding a case in `getRouteConfig()`.
- **Don't reference undeclared names in SQL** — they produce 500 at runtime; catch
  them in tests.
- **Don't concatenate user values into SQL strings.** Not even `fmt.Sprintf`. Always `{{name}}`.
- **Don't add `output_template` when using `response_content_type: $filetype`.**
- **Don't manually register an HTTP route for a connection's `subscribe_path`** — the
  server does it automatically in `registerSubscribeRoutes`.
- **Don't use `db.QueryRow` / raw `database/sql` calls in new code** — go through
  `StorageRef.Execute` so the parameterisation layer is always in the path.

---

## Common mistakes and fixes

| Symptom | Likely cause | Fix |
|---|---|---|
| Template renders but result is `null` | `isSingleRowQuery` returned false, `.Data` is a slice but template accesses `.Data.field` | Add `LIMIT 1` to the query |
| `undeclared input "x"` 500 error | SQL references `{{x}}` but `x` not in `inputs:` or step `inputs:` map | Declare `x` in the inputs block |
| Pipeline step gets wrong value | Dot-path points to wrong accumulator key | Check that previous step's `as:` name matches the path prefix |
| INSERT result gives `null` Data | Correct — INSERT returns nil Data. Use `.LastInsertID` | Access `{{.LastInsertID}}` in output_template |
| Multi-statement only runs first statement | Old code path without `splitStatements` | Ensure the SQLite backend is the current version with `executeSQL` dispatcher |
| SSE events not received | Broker published before client subscribed | Verify ring buffer replay; add `buffer_size` on the connection config |
| Task store input not found | `store.inputs` key doesn't exist in emitted JSON payload | Check the plugin's actual response shape; adjust dot-path |
| Scheduler job fails silently | `action.type` typo or plugin not registered | Check logs; validate at `startScheduler` time |

---

## Example reference

Every demo in `examples/apps/` is a complete, runnable `server.yaml`. Read them before
writing new YAML — they cover the full feature surface:

| demo | what it shows |
|---|---|
| `pipeline-demo` | multi-step storage-access with dot-path inputs |
| `pastebin` | multi-statement UPDATE+SELECT, single-row access, if_empty_status |
| `background-task-demo` | type: task, SSE publish, store write |
| `scheduled-jobs-demo` | action: plugin + then: publish + then: storage |
| `kv-store` | simple GET/PUT with if_empty_status: 404 |
| `sse-chat` | connections, stream-publish, SSE frontend |
| `file-uploads` | type: file input, $filetype response |
| `outbox-reliability-demo` | durable outbox, forward, plugin chain |
| `magic-link-login` | auth flows, magic-link routes |
| `url-shortener` | redirect route, INSERT + LastInsertID |
