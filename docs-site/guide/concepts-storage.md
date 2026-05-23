# Storage

Wave talks to databases through a `storage:` registry. Each entry
is a named backend; routes reference them by name (`source: app`).

```yaml
storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      users: { columns: ["id INTEGER PRIMARY KEY", "name TEXT"] }
```

## Supported backends

| `type` | Built-in | Notes |
|---|---|---|
| `sqlite` | ✅ | Single-file DB, perfect for small-to-mid apps |
| `postgres` | via plugin | Use `postgres-storage` plugin (or roll your own) |
| Any other | via plugin | Wave's storage interface is a small contract |

## Table declarations are migrations

```yaml
tables:
  users:
    columns:
      - id         INTEGER PRIMARY KEY AUTOINCREMENT
      - email      TEXT NOT NULL UNIQUE
      - created_at TEXT NOT NULL DEFAULT (datetime('now'))
    indexes:
      - "CREATE INDEX IF NOT EXISTS idx_users_email ON users(email)"
```

On boot, Wave runs `CREATE TABLE IF NOT EXISTS …` for every entry.
Column changes need an explicit migration:

```sh
wave migrate up --db ./data.db --dir ./migrations
```

See the [migrations docs](https://github.com/luowensheng/wave/blob/main/docs/storage-plugins.md)
for the file-numbering convention.

## SQL: the golden rule

**Every value in a SQL `execute:` string must come through `{{name}}`
parameterised binding.** Never use dot-notation or raw literals.

```yaml
# ✅ Good — {{user_id}} becomes ?
execute: "SELECT * FROM users WHERE id = {{user_id}}"

# ❌ NEVER — dot-notation interpolates the literal value, SQL injection
execute: "SELECT * FROM users WHERE id = {{.user_id}}"
```

The `{{name}}` function is registered against the SQL template
funcmap; it appends to the params slice and emits `?`. Bypassing it
defeats the whole strict-scope model.

## SQL template helpers

| Helper | Emits | Notes |
|---|---|---|
| `{{name}}` | `?` | primary — binds a declared input |
| `{{value "name"}}` | `?` | alias |
| `{{getCurrentTime}}` | `?` | server UTC ISO-8601 timestamp |
| `{{getCurrentTimeLocal}}` | `?` | local timezone |
| `{{addDays N}}` | `?` | now + N days |
| `{{formatTime "layout"}}` | `?` | custom format |
| `{{wrap "pattern"}}` | `?` | value with prefix/suffix, e.g. `%name%` for LIKE |
| `{{getUser}}` | `?` | authenticated user from session |
| `{{getClientIP}}` | `?` | best-guess source IP |
| `{{hasvalue "name"}}` | bool | guard for conditional clauses |
| `{{hasvalues "a" "b"}}` | bool | AND of presence checks |
| `{{hasanyvalue "a" "b"}}` | bool | OR |
| `{{iterlist "name"}}` | `[]string` | iterate a `type: array` input |
| `{{jsonArray (raw "name")}}` | `'[...]'` | JSON literal for `json_each` |
| `{{raw "name"}}` | Go value | escape hatch — only used inside `jsonArray` |
| `{{error "msg"}}` | aborts | render-time abort |

## Pipeline (multi-step) storage-access

For multi-query flows (look up X, then use X to fetch Y), use
`steps:` instead of a single `execute:`:

```yaml
storage-access:
  steps:
    - source: app
      inputs: { user_id: "" }           # "" → key in accumulator
      execute: "SELECT id, plan FROM users WHERE id = {{user_id}} LIMIT 1"
      as: user

    - source: app
      inputs: { uid: user.id }           # dot-path into prior result
      execute: "SELECT * FROM orders WHERE user_id = {{uid}}"
      as: orders

  output_template: '{"user":{{toJSON .user}},"orders":{{toJSON .orders}}}'
```

The accumulator is seeded with all declared request inputs (keyed
by their name), then each step's result lands under `as:`. Dot-paths
into the accumulator look like:

- `""` — the bare key name (just the input)
- `"."` — the whole accumulator (JSON-encoded as one param)
- `"user.id"` — `accum["user"].id`
- `"orders.0.item"` — `accum["orders"][0].item`

See the [`pipeline-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/pipeline-demo).

## Single-row vs multi-row

Wave detects single-row queries (presence of `LIMIT 1`, aggregate
without `GROUP BY`, scalar subquery) and exposes `.Data` as a map
(map field access in templates). Otherwise `.Data` is a slice of
maps.

```yaml
# Single-row — .Data.column
execute: "SELECT id, name FROM users WHERE id = {{id}} LIMIT 1"
output_template: '{"id":{{.Data.id}},"name":"{{.Data.name}}"}'

# Multi-row — {{toJSON .Data}}
execute: "SELECT id, name FROM users ORDER BY id"
output_template: '{{toJSON .Data}}'
```

## Multi-statement SQL

`execute:` can contain multiple `;`-separated statements. All but
the last run for side effects; the last drives the response.

```yaml
execute: |
  UPDATE pastes SET views = views + 1 WHERE slug = {{slug}};
  SELECT slug, content, views FROM pastes WHERE slug = {{slug}} LIMIT 1
```

The framework counts `?` placeholders per statement so params
distribute correctly across the split.

## Plugin-backed storage

For Postgres, Redis, ClickHouse, etc., declare a plugin and
reference it from `storage:`:

```yaml
plugins:
  pg:
    kind: subprocess
    command: ["/usr/local/bin/wave-postgres-storage"]

storage:
  app:
    type: plugin
    plugin: pg
    config: { dsn: "${env:DATABASE_URL}" }
```

See [`postgres-plugin-crud`](https://github.com/luowensheng/wave/tree/main/examples/apps/postgres-plugin-crud)
for a complete example and the
[storage plugin contract](https://github.com/luowensheng/wave/blob/main/docs/storage-plugins.md).

## See also

- [Routes](/guide/concepts-routes) — `type: storage-access`
- [JSON API recipe](/cookbook/json-api) — full CRUD walkthrough
- [Pipelines docs](https://github.com/luowensheng/wave/blob/main/docs/composition-and-pipelines.md)
