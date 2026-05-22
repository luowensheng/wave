# Outbound Requests, Pipelines, Workers & Composition

This guide covers the capabilities added for building real applications
out of small, explicit pieces:

- **`requests:`** — named, reusable outbound HTTP request definitions
- **`type: fetch`** — a route that calls an outbound request, runs side-effects, returns a templated response
- **Enhanced `schedule:`** — timed jobs with an `action` and a chain of `then:` sinks
- **Sinks** — `storage`, `publish`, `plugin`, `api`, and `for_each`
- **Pipeline `storage-access`** — multi-step DB queries with explicit per-step inputs
- **`type: task`** — fire-and-forget background work with SSE + persistence
- **Typed resource libraries (`kind:`) + `extern:` + `include:`** — split one app across many files, share a DB/plugin/broker, mount modules under prefixes

> **One rule underpins all of it: nothing is implicit.** Every input
> declares where it comes from, every output declares its content type,
> every shared resource has exactly one owner, and there are no
> "convenient" shorthands. If the server can't tell what you meant, it
> refuses to start with an error that names the fix.

---

## 1. The accumulator and its namespaces

Several features (fetch, schedule, pipelines, tasks) build a value map
called the **accumulator** and resolve **dot-paths** into it. Every path
is `<namespace>.<field>` — the namespace tells you exactly where the
data came from. There is no `""` shorthand and no bare `.`.

| Namespace | Set by | Example path |
|---|---|---|
| `inputs` | a route's `inputs:` block | `inputs.user_id`, `inputs.city` |
| `<output>` | an action's `output:` name | `weather.status`, `result.json` |
| `<as>` | a pipeline step's `as:` name | `user.id`, `orders.0.item` |
| `event` | a `type: task` emitted payload | `event.content` |

```yaml
# WRONG — rejected at boot
inputs:
  user_id: ""            # what is this? write the path explicitly
  data: "."              # no bare-accumulator shorthand

# RIGHT — every source is legible to someone new to the system
inputs:
  user_id: inputs.user_id
  temp:    weather.json.temperature
```

Non-scalar resolved values (maps, slices) are JSON-encoded to a string
automatically before being bound as a SQL parameter.

---

## 2. `requests:` — named outbound HTTP definitions

Top-level block. Each entry is a reusable request, optionally loaded
from an external file (compatible with the `requests` project format).

```yaml
requests:
  get-weather:
    url: "https://api.weatherapi.com/v1/current.json?key=$WEATHER_KEY&q={{city}}"
    method: GET
    headers:
      Accept: application/json
    timeout: 10          # seconds (default 30)
    retry_count: 2        # network-error retries (default 0)
    retry_delay: 1        # seconds between retries (default 1)
  post-notify:
    file: ./requests/notify.yaml     # base definition; inline fields below override it
    headers:
      Authorization: "Bearer $API_TOKEN"
```

`$ENV_VAR` in `url` and header values is expanded from the environment.
`{{varname}}` in `url`/`body` is substituted from the caller's `vars:`.

### The result shape — type-explicit, no implicit merge

Every outbound call (via a `type: fetch` route, a schedule `action:
api`, or an `api` sink) returns a map whose **field names encode their
types** so a reader never has to guess:

| Field | Type | Present |
|---|---|---|
| `text` | string | always — the raw response body |
| `json` | map | only when the body parses as a JSON object |
| `xml` | map | only when the body parses as well-formed XML |
| `status` | int | always — HTTP status code |
| `status_text` | string | always — e.g. `"200 OK"` |
| `headers` | map[string]string | always |

**XML→map rules** (explicit and deterministic — the rules are the
contract, there is no magic; uses the stdlib `xml.Decoder`, no deps):

- An element's key is its **local name**: namespace prefixes are
  stripped (`content:encoded` → `encoded`, `atom:link` → `link`,
  `dc:creator` → `creator`).
- A leaf element with only text → its trimmed **text string**.
- An element with child elements and/or attributes → a `map`.
- Attributes → keys prefixed with `@` (`<link href="x"/>` →
  `{"@href":"x"}`).
- Mixed text + children/attrs → the text goes under key `#text`.
- **Repeated sibling elements with the same name → a list in document
  order; a name occurring exactly once is not wrapped.** (Standard
  generic XML→map convention; the known single-vs-many edge.)
- The root element is the top-level key — an RSS doc is
  `resp.xml.rss.channel.item`, Atom is `resp.xml.feed.entry`.
- Malformed / non-XML body → `xml` absent (same as `json`).

```yaml
inputs:
  body:        resp.text          # string — unambiguous
  code:        resp.status        # int
  temp:        resp.json.temp_c   # explicit path into the parsed object
  trace:       resp.headers.X-Trace-Id
```

There is **no** top-level merge of parsed JSON keys — you always write
`resp.json.<field>`.

---

## 3. `type: fetch` — outbound call as a route

Declared inputs → outbound request → optional side-effect sinks →
explicitly templated response. `output_template` and
`response_content_type` are **required** (no implicit serialization).

```yaml
# GET — query inputs only (no body, so no expected_content_type)
- path: /api/weather
  method: GET
  type: fetch
  inputs:
    - { name: city, source: query, type: string, required: true }
  fetch:
    action:
      type: api
      ref: get-weather           # a name from requests: (or inline url/method/...)
      vars:
        city: inputs.city        # dot-path into the accumulator
      output: weather            # result stored as accum["weather"]
    then:
      - type: storage
        source: my_db
        inputs:
          city: inputs.city
          data: weather.text
        execute: "INSERT OR REPLACE INTO weather_cache(city,data) VALUES({{city}},{{data}})"
    response_content_type: application/json
    output_template: '{"temp":{{.weather.json.current.temp_c}}}'

# POST — accepts a body, so expected_content_type is declared
- path: /api/notify
  method: POST
  type: fetch
  expected_content_type: application/json
  inputs:
    - { name: message, source: body, type: string, required: true }
  fetch:
    action: { type: api, ref: post-notify, vars: { message: inputs.message }, output: result }
    response_content_type: application/json
    output_template: '{"ok":{{.result.json.ok}}}'
```

A global default for `expected_content_type` can be set once in the
top-level `default:` block; per-route overrides win.

```yaml
default:
  port: 8080
  expected_content_type: application/json   # applies to all body-accepting routes
```

---

## 4. Enhanced `schedule:` — action + then

The `schedule:` block is a **map** keyed by job name (consistent with
`plugins:`, `connections:`, etc.). A job is either the legacy
plugin-only form or the `action:` + `then:` form.

```yaml
schedule:
  poll_prices:
    every: 5s                      # or  at: "02:30"
    action:
      type: api                    # api | plugin | storage
      url: "https://api.example.com/prices"
      method: GET
      output: result               # REQUIRED when then: is non-empty
    then:
      - type: storage
        source: my_db
        inputs:
          price: result.json.price
        execute: "INSERT INTO prices(price) VALUES({{price}})"
      - type: publish
        connection: market_feed
        event_type: price_update
```

**Action types:** `api` (outbound HTTP, inline or `ref:`), `plugin`
(calls a registered plugin), `storage` (runs SQL; result under `data`).
`output:` names where the result lands in the accumulator and is
**required** whenever `then:` has sinks.

---

## 5. Sinks

A sink consumes the accumulator. Available in `schedule.then:` and
`fetch.then:`.

### `storage`
Resolve `inputs:` dot-paths → strict-scope DataLoader → parameterised
SQL. Same `{{name}}` → `?` rule as everywhere; never dot-notation.

```yaml
- type: storage
  source: db
  inputs: { id: item.id, body: resp.text }
  execute: "UPDATE jobs SET body={{body}} WHERE id={{id}}"
```

### `publish`
JSON-marshal the accumulator and publish to a named SSE/WS broker.

```yaml
- type: publish
  connection: feed
  event_type: tick
```

### `plugin`
JSON-marshal the accumulator as the request body and call a plugin.

```yaml
- type: plugin
  plugin: notifier
  trigger_key: new_price
```

### `api`
Make an outbound HTTP call from inside the pipeline. Same `ref:`/inline
shape and result shape as a `type: fetch` action. `output:` stores the
result for later sinks in the same iteration.

```yaml
- type: api
  ref: fetch_feed
  vars: { url: task.feed_url }
  output: resp
```

### `for_each`
Iterate an array in the accumulator and run a nested sink pipeline per
element. This is the queue-worker primitive — it turns "peek N rows,
process each" into pure YAML.

```yaml
- type: for_each
  in: peek.data            # dot-path to an array
  as: task                 # each element → accum["task"] for this iteration
  do:
    - { type: api, ref: fetch_feed, vars: { url: task.feed_url }, output: resp }
    - type: storage
      source: db
      inputs: { url: task.feed_url, body: resp.text }
      execute: "INSERT OR IGNORE INTO cache(url,body) VALUES({{url}},{{body}})"
    - type: storage
      source: db
      inputs: { id: task.id }
      execute: "UPDATE queue SET status='done' WHERE id={{id}}"
```

Each iteration gets its own accumulator copy — a sink's `output:` write
in one iteration never leaks into a sibling. Nested `do:` sinks are
validated recursively at boot.

> **Complete worked example:** `examples/apps/queue-worker-demo/` — a
> full DB-backed queue drained every 5s with `for_each` + `api`, no Go
> and no plugin.

---

## 6. Pipeline `storage-access`

Chain multiple DB queries; each step declares exactly which accumulator
values its SQL may use.

```yaml
- path: /user-orders/{user_id}
  method: GET
  type: storage-access
  inputs:
    - { name: user_id, source: path, type: int, required: true }
  storage-access:
    steps:
      - source: shop
        inputs:
          uid: inputs.user_id      # request input
        execute: "SELECT id,name FROM users WHERE id={{uid}} LIMIT 1"
        as: user
      - source: shop
        inputs:
          uid: user.id             # previous step's result
        execute: "SELECT * FROM orders WHERE user_id={{uid}}"
        as: orders
    response_content_type: application/json
    output_template: '{"user":{{toJSON .user}},"orders":{{toJSON .orders}}}'
```

A step's `inputs:` value is a dot-path; an empty string is a boot error
(write `inputs.user_id`, never `""`).

---

## 7. `type: task` — background work

Returns `202 {"task_id":"..."}` immediately, then runs the plugin call
in a detached goroutine that publishes to an SSE broker and optionally
persists each emitted payload. Survives client disconnects.

```yaml
- path: /api/process
  method: POST
  type: task
  inputs:
    - { name: prompt, source: body, type: string, required: true }
  task:
    plugin: my_llm
    trigger_key: chat
    streaming: true            # read plugin body as ndjson, emit per line
    connection: events         # required — every task publishes somewhere
    event_type: token
    store:
      source: db
      inputs:
        content: event.content # dot-path into the emitted JSON payload
      execute: "INSERT INTO messages(content) VALUES({{content}})"
```

The emitted payload is exposed under the `event` namespace
(`event.<field>`); a non-JSON payload becomes `{"data": "<raw>"}`.

---

## 8. Composition — typed libraries, `extern:`, `include:`

Split one app across many files. Each file still runs standalone.

### Typed resource libraries

A shared resource lives in a **typed library**: a file whose `kind:`
declares its single purpose and replaces the wrapper key. It contains
only that kind — no routes, no `default:`, no `include:`. Running
`wave serve` on a library is refused with an explicit error.

```yaml
# shared/app-db.yaml — the ONE owner of `db` and its tables
kind: storage
db:
  type: sqlite
  path: ./data/app.db
  tables:
    posts:     { columns: [ "id INTEGER PRIMARY KEY", "url TEXT" ] }
    rss_queue: { columns: [ "id INTEGER PRIMARY KEY", "feed_url TEXT UNIQUE", "status TEXT" ] }
```

Valid kinds: `storage`, `plugins`, `connections`, `auth`, `requests`,
`limits`. Library and module files may be **YAML or JSON,
interchangeably** — a `.json` module can borrow from a `.yaml` library.

### `extern:` — borrow a resource at its use-site

A module names each shared resource explicitly where it uses it. The
target's `kind:` must match the slot; a missing file or name is a boot
error. `extern` plus inline fields on the same entry is an error (no
silent merge).

```yaml
# modules/rss.yaml — runnable standalone; externs resolve by loading the libs
storage:
  db:     { extern: ../shared/app-db.yaml }   # borrow; contributes ZERO tables
plugins:
  gemini: { extern: ../shared/llm.yaml }
connections:
  feed:   { extern: ../shared/events.yaml }
routes:
  - path: /feeds
    method: GET
    type: storage-access
    storage-access:
      source: db
      execute: "SELECT url,title FROM rss_cache LIMIT 50"
      response_content_type: application/json
      output_template: "{{toJSON .Data}}"
```

**Schema ownership:** the `kind: storage` library owns the DB *and all
its tables*. Borrowers get read/write but contribute no DDL.

### `include:` — compose modules under a prefix

The host server file merges whole modules. `prefix:` is declared only
at the include site — the host decides the layout; the module never
presumes its mount point.

```yaml
# app.yaml — the only "server" file
default: { port: 9939, expected_content_type: application/json }
include:
  - { file: ./modules/auth-routes.yaml }            # no prefix → mounts at /
  - { file: ./modules/rss.yaml, prefix: /rss }      # /feeds → /rss/feeds
```

Prefix rewrites every **inbound, module-authored** path: `Route.Path`,
auth `redirect_on_success`/`failure`, magic-link callback/redirect, and
authored `subscribe_path`. Outbound URLs (`forward_url`, upstream
`api.url`, OAuth `redirect_uri`) are never rewritten. Nested includes
compose: `/a` + `/b` + `/feeds` → `/a/b/feeds`.

A **borrowed** (`extern`) connection keeps its canonical
`subscribe_path` — you don't own it, so the prefix doesn't move it.

### `absolute: true` — opt a route out of prefixing

For OAuth callbacks / webhook paths registered with a third party, pin
the literal path:

```yaml
- path: /oauth/callback
  method: GET
  type: oauth-callback
  absolute: true             # stays /oauth/callback even under a prefix
  oauth-callback: { for: google }
```

### Strict conflict detection

Identity is `(resolved library path, kind, name)`. The same library
borrowed by N modules is **one** definition — no conflict. Two
*different* files authoring the same resource name is a boot error:

```
boot error: resource storage/db authored by two libraries:
  - shared/app-db.yaml      (borrowed via modules/rss.yaml)
  - modules/auth-routes.yaml (authored here)
fix: borrow it →  storage: { db: { extern: ../shared/app-db.yaml } }
```

Referencing is always fine; only re-authoring a name errors.

> **Complete worked example:** `examples/composition/` — typed libs,
> a module externing them and runnable alone, and a host `app.yaml`
> composing under prefixes.

### Every tool sees the composed view

`wave serve`, `wave routes`, `wave validate`, `wave doctor`, the
`/openapi.json` endpoint, and the Studio UI all run the same resolver,
so they show the merged, prefixed, deduplicated app — never a partial
or pre-composition view. A typed-library file is reported as
"not a server" consistently across all of them.

---

## Quick reference — boot-time errors you'll see (by design)

| Mistake | Error |
|---|---|
| `inputs:` value is `""` or `.` | `from-path is empty — write it explicitly` |
| `type: fetch` missing `output_template` | `output_template is required` |
| `schedule` action with `then:` but no `output:` | `action.output is required when then: is non-empty` |
| `extern:` target kind ≠ slot | `extern target X is kind:plugins, expected kind:storage` |
| Two files author the same resource | `resource storage/db authored by two libraries: ...` |
| `include:` cycle | `include cycle: a.yaml -> b.yaml -> a.yaml` |
| `wave serve` on a `kind:` file | `is a kind:storage library, not a server` |
| Borrowed/missing resource name | `name "db" not found in library /abs/path` |

Fail-fast at boot is intentional: a misconfigured app never starts
serving 500s — it tells you the exact fix before it accepts traffic.
