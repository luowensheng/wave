# Templating unification — design proposal

Status: **proposal / not yet implemented**

Wave today has at least four distinct templating dialects mixed
across YAML configs. This document captures the actual state,
explains why every divergence exists, and proposes a unified model
that preserves the safety properties while removing the cognitive
load.

This is a planning document. No code changes yet. Implementation
will land in phases (Phase 1 is additive, Phase 2 deprecates the
old forms, Phase 3 removes them at v1.0).

## TL;DR — the design call (maintainer decision)

> **There is exactly one way to get a value into a template: declare
> it as an input.**

Today's value-producing helpers (`{{getCurrentTime}}`, `{{getUser}}`,
`{{getClientIP}}`, `{{wrap}}`, `{{value}}`, `{{bind}}`, `{{get}}`,
`{{addDays}}`, `{{formatTime}}`, etc.) **all go away**. They're
replaced by new `source:` types on the `inputs:` block — `time`,
`user`, `client_ip`, `request_id`, `host`, `random`, `uuid`,
`literal` — and a small set of `transform:` options (`wrap`, `lower`,
`upper`, `trim`, `format_time`, `coalesce`).

The template surface shrinks to:
- **`{{name}}`** — reference a declared input (binds in SQL; renders
  to string elsewhere)
- **Control flow** — `{{if has "x"}}`, `{{range iter "x"}}`,
  `{{error "msg"}}`
- **SQL fragments** — `{{jsonArray "x"}}`, `{{raw "x"}}` (produce SQL
  text that isn't a bindable value)
- **Output serialization** — `{{toJSON x}}`, `{{escape x}}`,
  `{{urlPathEscape x}}`, `{{urlQueryEscape x}}` (render-mode only)

That's the whole vocabulary. ~12 helpers total, all in clear
categories. Everything else is an input.

### Before / after — a worked example

```yaml
# TODAY — three different value-producing mechanisms in one route
inputs:
  - { name: text, source: body, type: string, required: true }
storage-access:
  execute: |
    INSERT INTO audit_log(actor, search_pattern, ip, at)
    VALUES (
      {{getUser}},                  -- helper
      {{wrap "%text%"}},            -- helper with arg
      {{getClientIP}},              -- helper
      {{getCurrentTime}}            -- helper
    )
```

```yaml
# AFTER — every value declared as an input; template only has {{name}} refs
inputs:
  - { name: text, source: body,      type: string, required: true }
  - { name: pat,  source: body,      type: string, required: true,
                  field: text,       transform: wrap, pattern: "%{}%" }
  - { name: me,   source: user }                      # default field: id
  - { name: ip,   source: client_ip }
  - { name: now,  source: time }                      # default ISO-8601 UTC
storage-access:
  execute: |
    INSERT INTO audit_log(actor, search_pattern, ip, at)
    VALUES ({{me}}, {{pat}}, {{ip}}, {{now}})
```

Two more lines of `inputs:` declaration, four fewer template
helpers. Every value the SQL touches is named and declared. The
template only references declared names. Strict scope by
construction — same safety property as today, just consistent across
the whole framework instead of split between "SQL has helpers" and
"output has dot-form".

---

## 1. The actual current state

What you can write inside a single `server.yaml` today:

| Syntax | Example | Where it works | Evaluator | When |
|---|---|---|---|---|
| `${ENV:NAME}` | `secret: "${ENV:STRIPE_WEBHOOK_SECRET}"` | Any string value | `infra/secrets` | **Boot-time** |
| `${ENV:NAME:default}` | `port: "${ENV:PORT:8080}"` | Any string value | `infra/secrets` | Boot-time |
| `${FILE:/path}` | `key: "${FILE:/etc/wave/jwt.pem}"` | Any string value | `infra/secrets` | Boot-time |
| `${PLUGIN:name:uri}` | `dsn: "${PLUGIN:vault:secret/data/pg#dsn}"` | Any string value | `infra/secrets/plugin` | Boot-time |
| `{name}` | `path: /users/{id}` | Route `path:` and forward URLs | Go 1.22 ServeMux + `strings.ReplaceAll` | Request-time |
| `{{name}}` (function call) | `execute: "SELECT … WHERE id={{user_id}}"` | SQL `execute:` | `infra/sqlite` funcMap | Request-time, **binds to `?` (parameterised)** |
| `{{.name}}` (dot-form) | `output_template: '{"id":{{.Data.id}}}'` | `output_template` (storage-access, fetch, run-process), error templates | `infra/render.Render` | Request-time, **literal string interpolation** |
| `{{varname}}` (simple) | `body: '{"city":"{{city}}"}'` in `requests:` | HTTP client request bodies + URLs | `infra/httpclient.substituteVars` | Request-time, no functions |
| `{{call .X | .Y}}` | (Go template builtins — `call`, `range`, `if`, `with`) | Wherever `infra/render` is used | Go `text/template` | Request-time |
| (literal — no templating) | `content.body`, `redirect.redirect_url`, `headers` values | Several route types | none | n/a |

That's **at minimum five evaluators** with three different timings
and inconsistent helper sets.

---

## 2. Concrete inconsistencies

Found by exhaustive scan of `infra/render`, `infra/sqlite`,
`infra/httpclient`, `infra/secrets`, and every YAML field that
takes a templatable value.

### 2.1 Dot-form is required in some contexts, **forbidden** in others

The framework's biggest cognitive load:

```yaml
# SQL: function-form ONLY. {{.user_id}} would interpolate the value as a literal —
# SQL injection. CLAUDE.md says NEVER use dot-form here.
storage-access:
  execute: "SELECT * FROM users WHERE id = {{user_id}}"

# output_template: dot-form is the idiomatic way to access result fields.
# function-form {{Data}} also works (every map key becomes a zero-arg
# function), but the convention is dot-form.
storage-access:
  output_template: '{"id":{{.Data.id}}, "name":"{{.Data.name}}"}'
```

**Why it's like this:** SQL templates use the strict-scope DataLoader
as the template context — DataLoader has no exported fields, so
`{{.anything}}` evaluates to nil. This is a safety property by
*construction*: the only way to access a value is through the
function-form (`{{user_id}}`), which goes through the binding path
(`?` + params). Removing the dot-form from SQL is impossible because
Go's `text/template` always allows it; we rely on "there's nothing
to access".

For `output_template`, the renderer passes a real `map[string]any`,
so both forms work. The dot-form became idiomatic because users
need to access **nested** result fields (`.Data.id`, `.Data.rows.0.name`)
and the function-form can't traverse nesting.

### 2.2 Two SQL aliases for the same operation

```yaml
execute: "INSERT INTO t(x) VALUES ({{name}})"            # function-call form
execute: "INSERT INTO t(x) VALUES ({{value \"name\"}})"  # explicit alias
```

Both bind a `?`. Two ways to write the same thing.

### 2.3 Path variables: three syntaxes for "a value from the URL path"

```yaml
- path: /users/{id}                       # Go mux pattern — required by stdlib
  inputs: [{ name: id, source: path, type: int, required: true }]
  storage-access:
    execute: "SELECT * FROM users WHERE id = {{id}}"   # accessed via declared input

# vs forward routes — uses raw {name} substitution in the URL, no
# input declaration required (and no validation):
- path: /proxy/{tenant}/api
  type: forward
  forward:
    forward_url: "http://{tenant}.upstream.local/api"     # raw {name}, no escaping

# vs redirect routes — no templating at all:
- path: /old/{id}
  type: redirect
  redirect:
    redirect_url: "https://new.example.com/{id}"          # literal — {id} NOT substituted
```

Same field shape (a URL), three different behaviors. Forward URLs
do raw substitution with no escaping. Redirect URLs do nothing.

### 2.4 `content.body` is literal but `output_template` is templated

```yaml
# Static — no way to interpolate values
- path: /
  type: content
  content:
    status_code: 200
    body: "Hello, world"          # literal; can't say {{getUser}} or {{.id}}

# Templated — full Go template renderer
- path: /
  type: storage-access
  storage-access:
    output_template: "Hello, {{getUser}}"
```

Users who want a templated response from a non-SQL route have to
fall through to `storage-access` even when there's no SQL to run.

### 2.5 HTTP client uses its own incompatible `{{...}}` syntax

```yaml
requests:
  weather_api:
    url:  "https://api.example.com/{{city}}"
    body: '{"city":"{{city}}"}'
```

Looks like Go template syntax. **It isn't.** `infra/httpclient/execute.go`
has its own simpler `substituteVars` that does flat string
replacement only — no functions, no `if`, no `range`, no nested
access. If you try `{{toJSON .city}}` it silently fails (the
literal text `{{toJSON .city}}` lands in the URL).

### 2.6 Helpers are scattered across contexts with no overlap

| Helper | Available in SQL? | Available in output_template? | Available in HTTP client? |
|---|:---:|:---:|:---:|
| `{{name}}` (input bind) | ✅ | ✅ (renders to string) | ⚠️ different impl |
| `{{getCurrentTime}}` | ✅ | ❌ | ❌ |
| `{{wrap}}` | ✅ | ❌ | ❌ |
| `{{jsonArray}}` | ✅ | ❌ | ❌ |
| `{{hasvalue}}` | ✅ | ❌ | ❌ |
| `{{getUser}}` | ✅ | ❌ | ❌ |
| `{{toJSON}}` | ❌ | ✅ | ❌ |
| `{{urlPathEscape}}` | ❌ | ✅ | ❌ |
| `{{escape}}` | ❌ | ✅ | ❌ |

Same `{{...}}` syntax, completely different callable sets in
different contexts. There's no way to use `{{toJSON x}}` in SQL
or `{{getCurrentTime}}` in `output_template`.

### 2.7 Headers, status codes, and content-types are static

```yaml
content:
  status_code: 200
  headers:
    - ["Content-Type", "application/json"]
    - ["Cache-Control", "no-store"]
  body: "..."
```

The `status_code`, `headers`, and `Content-Type` are all
config-time literals. There's no way to:
- Set status based on a query result (`if row.is_premium then 200 else 402`)
- Set a header from a captured value (`X-User-Id: {{getUser}}`)
- Vary content-type by `Accept` header

Some route types (`storage-access` with `if_empty_status: 404`)
work around this for the single specific case of "empty result".

### 2.8 Timing is mixed in the same value

```yaml
auth:
  app:
    secret: "${ENV:JWT_SECRET}"    # boot-time
    cookie_max_age_seconds: 86400  # literal

routes:
  - path: /me
    auth: [app]
    type: storage-access
    storage-access:
      execute: "SELECT * FROM users WHERE id = {{getUser}}"   # request-time
```

Three different evaluation phases in one route. The user has to
remember which syntax means "now (at boot)" vs "later (per
request)". `${ENV:NAME}` and `{{name}}` look totally different,
which is actually good — but the mental model is still triple.

---

## 3. Why this happened (charitably)

Each evaluator was added when it solved a real problem the others
couldn't:

- **`${ENV:…}`** — needed at boot to resolve secrets *before* anything else parses the config
- **`{{name}}` in SQL** — needed parameter binding (safety-critical)
- **`{{.x}}` in output_template** — needed nested traversal of query results
- **HTTP client's `substituteVars`** — added before Go templates were imported there; never refactored
- **`{name}` in forward URLs** — added when forward routes needed dynamic targets; the implementer chose path-var syntax to match the route pattern

Each individual choice was reasonable. The cumulative result is
five languages.

---

## 4. Proposed unification

### 4.1 The principle

> **There is exactly one way to get a value into a template:
> declare it as an input.**

The `inputs:` block already declares values from the request (path,
query, body, headers, cookies). It expands to cover *every* value
source — including the things that today are helpers
(`{{getCurrentTime}}`, `{{getUser}}`, `{{getClientIP}}`, etc.).

The templating language shrinks to:
1. **`{{name}}`** — reference a declared input. Works the same in
   every context (binds in SQL; renders to string elsewhere).
2. **Control flow** — `{{if has "x"}} … {{end}}`, `{{range iter "x"}}`,
   `{{with x}}`. These don't produce values; they shape the output.
3. **SQL fragment generators** — `{{jsonArray "x"}}` produces a raw
   SQL literal (not a bindable value), so it has to be a function.
   Very few of these.
4. **Output serializers** — `{{toJSON x}}`, `{{escape x}}`,
   `{{urlPathEscape x}}`, `{{urlQueryEscape x}}` — render a declared
   value to a specific wire format. Render-mode only; never in SQL.

That's it. No `{{getCurrentTime}}`, no `{{getUser}}`, no
`{{getClientIP}}`, no `{{wrap}}`, no `{{value}}`. All of those
become declarable input sources or transforms on inputs.

### 4.2 The seven detailed principles

1. **`{{name}}` is the only value-access syntax in templates.**
   In SQL contexts it binds (`?` + param). In other contexts it
   renders to string. Always references a declared input.
2. **Dot-form (`{{.x.y}}`) is reserved for nested *result* traversal.**
   `.data`, `.last_insert_id`, `.rows_affected`. Inputs are always
   `{{name}}`, never `{{.name}}`. (The two have non-overlapping
   namespaces — `inputs` and `result` are separate.)
3. **Every value, including server-derived ones, is a declared input.**
   New `source:` types — `time`, `user`, `client_ip`, `request_id`,
   `host`, `random`, `uuid` — produce values at request time the
   same way `source: query` does.
4. **Templating is universal across string fields.**
   `content.body`, `redirect.redirect_url`, header values — anywhere
   a string is a config value, it can be templated. Literal-only
   fields are removed.
5. **`${...}` stays at boot, `{{...}}` runs at request.**
   The one visual distinction worth keeping.
6. **Path variables are inputs.**
   `{name}` in `routes[].path:` is stdlib syntax (immutable). Accessing
   it everywhere else is `{{name}}` via a declared `source: path`
   input. Forward URLs migrate from raw `{name}` to `{{name}}`.
7. **One renderer, two modes — and the modes only differ in what
   `{{name}}` does with the value.**
   - **render-mode** (default): `{{name}}` renders value to a string
   - **bind-mode** (SQL only): `{{name}}` emits `?` + binds value as
     a SQL parameter
   Helpers (control flow + fragment generators) work the same way in
   both modes.

### 4.3 Standardized data shape

Every templated context — `output_template`, `content.body`,
`redirect.redirect_url`, header values, request bodies, error
templates — gets the **same** template-context shape:

```go
// Available in every templated context:
{{<input_name>}}        // Every declared input — function-form, preferred
                        // In SQL: binds. Elsewhere: renders to string.

.inputs.<name>          // Same values via dot-form (for iteration / nested access)
.request.method         // "GET" / "POST" / …
.request.path           // Raw URL path
.request.query.<k>      // Query string values
.request.headers.<k>    // Request header values
.request.cookies.<k>    // Cookie values
.request.path_vars.<k>  // Path variables (same as declared path inputs)
.request.client_ip      // Best-effort IP
.request.id             // X-Request-ID

.user                   // Authenticated user, or nil
.user.id
.user.email
.user.roles
.user.claims.<k>

.data                   // Route-result data (route-type specific):
                        //   storage-access: query result rows / row
                        //   fetch / api:    { status, headers, json, text }
                        //   task:           { task_id }

.last_insert_id         // INSERT routes
.rows_affected          // UPDATE/DELETE routes
.column_names           // SELECT routes
```

Two access forms for the **same** values, by namespace:
- Declared inputs → `{{name}}` (function) preferred; `.inputs.name` for iteration
- Result data → `.data.field` (dot-form) — declared inputs never collide because they live in a separate namespace

This kills 2.1 (the "forbidden in SQL, required in output" tension):
in **every** context, declared inputs use `{{name}}` and nested
results use `{{.data.x}}`. The SQL safety property holds because
`{{name}}` always binds (never interpolates literally) in bind-mode.

### 4.4 The new input source types

Every value that today is a template helper becomes an input
source. The full list of `source:` values:

**Request-derived** (already exist today):

| `source:` | Reads from | Example today | After |
|---|---|---|---|
| `path` | URL path variable | `path: /users/{id}` + `source: path` | unchanged |
| `query` | `?name=value` | `?q=hello` | unchanged |
| `body` | JSON body field | `{"name":"x"}` | unchanged |
| `form` | Form field | `name=x&age=10` | unchanged |
| `header` | Request header | `X-Tenant: acme` | unchanged |
| `cookie` | Cookie value | `session=abc` | unchanged |
| `body_raw` | Whole raw body | text/plain bodies | unchanged |

**Server-derived** (NEW — replace helpers):

| New `source:` | Replaces helper | What it provides |
|---|---|---|
| `time` | `{{getCurrentTime}}` | Server UTC time, ISO-8601 (format configurable) |
| `time_local` | `{{getCurrentTimeLocal}}` | Server local-tz time |
| `time_offset` | `{{addDays N}}` | Server time + duration (`offset: 7d`) |
| `user` | `{{getUser}}` | Authenticated user; `field:` picks attribute (`id`, `email`, etc.) |
| `client_ip` | `{{getClientIP}}` | Best-guess client IP |
| `request_id` | (currently no helper) | `X-Request-ID` value |
| `host` | (currently `{{pathVar "host"}}` hack) | Request `Host` header |
| `random` | (currently no helper) | Random bytes hex-encoded; `length: N` |
| `uuid` | (currently no helper) | New UUIDv4 |
| `literal` | (none) | Constant value declared in YAML; useful for defaults / version strings |

**Example — before and after:**

```yaml
# Today
inputs:
  - { name: text, source: body, type: string, required: true }
storage-access:
  execute: |
    INSERT INTO audit_log(actor, text, ip, at)
    VALUES ({{getUser}}, {{wrap "%text%"}}, {{getClientIP}}, {{getCurrentTime}})
```

```yaml
# After
inputs:
  - { name: text, source: body,      type: string, required: true }
  - { name: me,   source: user,      field: id }
  - { name: ip,   source: client_ip }
  - { name: now,  source: time }                       # UTC ISO-8601 by default
storage-access:
  execute: |
    INSERT INTO audit_log(actor, text, ip, at)
    VALUES ({{me}}, {{text}}, {{ip}}, {{now}})
```

Every value the SQL needs is in `inputs:`. The template only
references declared names. Strict scope by construction — same as
the existing `{{name}}` model, just extended to cover the things
that used to be helpers.

### 4.5 Input transforms (replaces a few more helpers)

The `wrap "%name%"` helper is really "transform this input's value
before binding it". Express as a `transform:` on the input
declaration:

```yaml
inputs:
  - { name: q, source: query, type: string, transform: wrap, pattern: "%{}%" }

storage-access:
  execute: "SELECT * FROM items WHERE name LIKE {{q}}"
```

Transforms available:

| `transform:` | Replaces helper | What |
|---|---|---|
| `wrap` | `{{wrap "%name%"}}` | `pattern: "%{}%"` → `%<value>%` |
| `lower` | (none) | Lowercase the string |
| `upper` | (none) | Uppercase the string |
| `trim` | (none) | Strip whitespace |
| `format_time` | `{{formatTime "layout"}}` | Reformat a time input |
| `default` | (input field already) | Fallback if absent |

Transforms apply after parsing/validation, before the value reaches
the template. They can compose (`transform: [lower, trim]`).

### 4.6 What remains as a template helper

Only **two** categories:

**Control flow** (don't produce values, shape the output):

| Helper | What |
|---|---|
| `{{if has "name"}} … {{end}}` | Renamed from `{{if hasvalue "name"}}` for brevity |
| `{{if hasAll "a" "b"}} … {{end}}` | AND |
| `{{if hasAny "a" "b"}} … {{end}}` | OR |
| `{{range iter "name"}} … {{end}}` | Iterate `type: array` input |
| `{{range iterKeys "name"}} … {{end}}` | Iterate `type: object` input keys |
| `{{error "msg"}}` | Abort render with error |

**SQL fragment generators** (produce SQL text, not bindable values
— only meaningful in bind-mode):

| Helper | What | Why it can't be an input |
|---|---|---|
| `{{jsonArray "name"}}` | Emits `'[1,2,3]'` SQL literal for `json_each` | The whole array becomes part of the SQL text, not a parameter |
| `{{raw "name"}}` | Escape hatch — access raw value without binding | Only used inside other helpers like jsonArray |

That's the whole template surface. Six control-flow helpers, two
SQL-fragment helpers, plus `{{name}}` for values.

### 4.7 Removed entirely

| Today | After |
|---|---|
| `{{getCurrentTime}}` | declare `source: time` |
| `{{getCurrentTimeLocal}}` | declare `source: time_local` |
| `{{addDays N}}` | declare `source: time_offset, offset: <N>d` |
| `{{formatTime "layout"}}` | declare `source: time, format: <layout>` (or `transform: format_time`) |
| `{{wrap "pattern"}}` | declare `transform: wrap, pattern: "..."` |
| `{{getUser}}` | declare `source: user, field: id` |
| `{{getClientIP}}` | declare `source: client_ip` |
| `{{pathVar "name"}}` | already covered by `source: path` |
| `{{value "name"}}` | use `{{name}}` (alias removed) |
| `{{bind expr}}` | use a declared input |
| `{{get "name"}}` | use `{{name}}` |
| `{{getindex "name" N}}` | declare a specific input with `transform: index, n: <N>` (or use `{{range iter ...}}`) |
| `{{hasvalue "name"}}` | renamed to `{{has "name"}}` |
| `{{hasvalues …}}` | renamed to `{{hasAll …}}` |
| `{{hasanyvalue …}}` | renamed to `{{hasAny …}}` |
| `{{iterlist "name"}}` | renamed to `{{iter "name"}}` |
| `{{itermap "name"}}` | renamed to `{{iterKeys "name"}}` |
| Output-template helpers (`toJSON`, `escape`, `urlPathEscape`, …) | become input transforms OR small helper set kept only in render-mode (see 4.7) |

### 4.8 Render-mode output helpers

Render-mode (output_template, content.body, headers, etc.) needs a
small set of *output* helpers — things that serialize a declared
input value into the wire format. These don't produce new values;
they transform an already-declared value at render time:

| Helper | What |
|---|---|
| `{{toJSON name}}` | JSON-serialize a value (typically a `type: object` or `type: array` input, or a result row) |
| `{{escape name}}` | HTML-escape |
| `{{urlPathEscape name}}` | URL path-segment escape |
| `{{urlQueryEscape name}}` | URL query-param escape |

These could ALSO be expressed as transforms on the input — but for
output the call-site location matters (`{{toJSON .data}}` vs
declaring `data_json` as a transformed copy). Keep them as
render-mode helpers; they're only relevant for output serialization
and never appear in SQL.

### 4.9 Per-field migration table

| Field | Today | Tomorrow |
|---|---|---|
| `routes[].path` | Go mux `{name}` | unchanged — stdlib requirement |
| `storage-access.execute` | `{{name}}`, no `{{.x}}` | unchanged (already the design we want) |
| `storage-access.output_template` | `{{.Data.x}}` + helpers | `{{.data.x}}` (lowercase) + helpers; backward-compat alias for `{{.Data.x}}` |
| `content.body` | **literal** | **templated** (additive — no break) |
| `content.headers[].value` | literal | templated |
| `content.status_code` | literal int | accepts int OR `{{template}}` rendering to int |
| `redirect.redirect_url` | **literal** | **templated** (additive) |
| `forward.forward_url` | `{name}` (raw) | `{{name}}` (via inputs); `{name}` kept as deprecated alias |
| `forward.include_headers[].value` | literal | templated |
| `auth_login.error_template_str` | `infra/render` with limited helpers | unified renderer with full helper set |
| `magic-link-request.email_template` | `infra/render` | unified renderer |
| `requests.<name>.url` | HTTP client's `{{var}}` flat substitution | unified renderer |
| `requests.<name>.body` | HTTP client's `{{var}}` flat substitution | unified renderer |
| `requests.<name>.headers[].value` | literal | templated |
| `plugin.command[]` | literal | unchanged (process spawn, not templated) |
| `plugin.env.<k>` | `${ENV:…}` boot-time only | unchanged — boot-time substitution |

### 4.10 Specific fix for `type: content`

The user explicitly called this out. Today:

```yaml
- path: /
  type: content
  content:
    status_code: 200
    headers: [["Content-Type", "text/plain"]]
    body: "hello"           # literal
```

After Phase 1:

```yaml
- path: /
  type: content
  inputs: [{ name: name, source: query, type: string, default: "world" }]
  content:
    status_code: 200
    headers: [["Content-Type", "text/plain"]]
    body: "Hello, {{name}}!"     # templated — uses the same renderer as storage-access
```

Same template engine, same helpers, same data shape. Existing
`content.body` configs keep working because they have no `{{...}}`
markers; the renderer is a no-op for purely literal strings.

### 4.11 Migration phases

**Phase 1 — additive, zero breaking changes.**
- New package `infra/template` with the unified funcMap.
- Wire it into `content.body`, `redirect.redirect_url`,
  `forward.forward_url`, header values, error templates.
- Document the unified shape.
- All existing `{{.Data.x}}` configs keep working (renderer
  accepts both `Data` and `data`).
- All existing `{name}` forward URLs keep working.
- HTTP client gains the full funcMap; bespoke `substituteVars`
  becomes a thin wrapper.

**Phase 2 — deprecation warnings.**
- `wave validate` and `wave doctor` emit warnings on:
  - `{{value "name"}}` in SQL (use `{{name}}`)
  - `{name}` in forward URLs (use `{{name}}`)
  - `{{.Data.x}}` in output_template (use `{{.data.x}}`)
  - Any usage of HTTP client's old syntax that the unified
    renderer would handle differently
- CHANGELOG lists every deprecation with the rewrite.

**Phase 3 — removal at v1.0.**
- Remove the deprecated aliases.
- `wave fmt` auto-rewrites old → new during the migration window.

### 4.12 Implementation skeleton

```
infra/template/
  template.go          — exposed Render(), RenderInto(), Compile()
  funcmap.go           — the one base funcMap
  context.go           — the standardized data shape (.request, .data, .user, .inputs)
  bind.go              — bind-mode shim (for SQL contexts)
  bind_test.go
  funcmap_test.go
  template_test.go

infra/sqlite/
  sqlite.go            — uses infra/template.Compile with bind-mode opt-in
  (delete its private funcMap)

infra/render/
  (delete; migrate callers to infra/template)

infra/httpclient/
  execute.go           — uses infra/template with render-mode (drop substituteVars)

infra/secrets/
  (unchanged — boot-time interpolation is a separate concern)
```

Estimated work:
- Phase 1: ~3-5 days of code + tests (the new package is small; the
  wiring touches many call sites).
- Phase 2: ~1 day to add the lint passes to `validate` / `doctor`.
- Phase 3: ~1 day at v1.0 to delete deprecated code paths.

---

## 5. Open questions

### Decided (after maintainer review)

- ~~**Should `{{value "name"}}` stay as an alias?**~~ → **No.** Removed
  at v1.0; `wave fmt` rewrites automatically during the migration
  window.
- ~~**Should we add `{{coalesce}}` and `{{default}}` helpers?**~~ → **No.**
  `default:` is already an input field. Multi-value fallback
  becomes `transform: coalesce, with: [a, b, c]` on an input.
- ~~**Built-in helpers vs input shadowing?**~~ → **Mostly moot** —
  there are very few helpers left (~8 control-flow + 4 output) and
  their names (`has`, `iter`, `error`, `toJSON`, …) don't collide
  with typical input names. We still error at compile time on
  collision.

### Still open

These need a decision before Phase 1 lands:

1. **Should `content.body` becoming templated be opt-in via a flag?**
   *Proposal: no — literal strings stay literal because they contain
   no `{{...}}` markers. The renderer is a no-op for them. Any
   string containing `{{` was already broken HTML or required
   manual encoding.*

2. **How do we handle `{{.Data.x}}` (capital D) vs `{{.data.x}}` (lowercase) during migration?**
   *Proposal: renderer accepts both during Phases 1–2. `wave fmt`
   rewrites to lowercase. v1.0 removes the capital form.*

3. **Templated status codes — useful or footgun?**
   `status_code: "{{if has \"premium\"}}200{{else}}402{{end}}"` is
   expressive but error-prone (renders to a string; needs int
   parsing). *Proposal: keep status codes as literal int by default;
   add a separate `status_template:` field for the rare case.*

4. **HTML auto-escape in render-mode for `text/html` responses?**
   Today nothing auto-escapes. We could detect `Content-Type:
   text/html` and switch to `html/template` semantics. Risk: silent
   behavior change. *Proposal: add an explicit `escape_html: true`
   field on `content:` and `output_template:`; default false.*

5. **Are there `source:` types we're missing?** The new server-derived
   list is `time`, `time_local`, `time_offset`, `user`, `client_ip`,
   `request_id`, `host`, `random`, `uuid`, `literal`. Are there
   common patterns that need their own source rather than a generic
   helper? Candidates: `session` (full session object), `route_id`
   (the matched route's id), `header_all` (whole headers map).

6. **For `source: time` — what's the default format?**
   *Proposal: ISO-8601 UTC (`2026-05-23T09:00:00Z`). Configurable via
   `format: "<go-time-layout>"` on the input. Common case stays
   one-liner.*

7. **For `source: user` — what's the default field?**
   *Proposal: `id`. So `{ name: me, source: user }` is shorthand for
   `{ name: me, source: user, field: id }`. Set `field: email` /
   `field: roles` / `field: claims.<key>` for others.*

---

## 6. What this fixes — concretely

After all three phases:

- ✅ One value-access syntax: `{{name}}` everywhere
- ✅ One data-shape: `.request`, `.data`, `.user`, `.inputs` available in every templated context
- ✅ One helper set: every helper works in every context (with bind-mode adapting the value evaluators in SQL)
- ✅ `content.body` is templated like everything else
- ✅ `redirect.redirect_url` is templated
- ✅ Headers can be templated
- ✅ Forward URLs use `{{name}}` not `{name}`
- ✅ HTTP client's `requests:` uses the same renderer
- ✅ SQL safety preserved by construction (bind-mode evaluator emits `?`, never a literal)
- ✅ `${ENV:…}` and `{{…}}` stay visually distinct — boot vs request
- ✅ Five evaluators → one base evaluator + one bind-mode shim

What stays the same:

- `${ENV:…}` / `${FILE:…}` / `${PLUGIN:…}` — boot-time only
- `{name}` in `routes[].path:` — Go stdlib requirement
- The strict-scope DataLoader safety property in SQL
- The `infra/secrets` package
- The data shape for `storage-access` query results (lowercased namespace)

---

## 7. Decision needed

Before any code lands, we need user/maintainer sign-off on:

- The principle that **`{{name}}` works in every context** (function-call for declared inputs)
- The principle that **`.data.x` (dot-form) is reserved for nested result traversal only**
- The migration phasing (Phase 1 additive → Phase 2 warn → Phase 3 remove)
- The 7 open questions in section 5

Once those are settled, Phase 1 is straightforward to implement.
The audit + this document is the spec.
