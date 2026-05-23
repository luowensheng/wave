# Use Supabase / Neon / Railway Postgres

You don't need to self-host Postgres — managed providers like
**Supabase**, **Neon**, **Railway**, **Render**, **Aiven**, and
**Crunchy** all give you a real Postgres with a connection string.
Wave talks to it via the [`postgres-storage` plugin](https://github.com/luowensheng/wave/tree/main/examples/plugins/postgres-storage).

Same `storage-access` SQL you'd write against SQLite works against
managed Postgres — change one line in `server.yaml`.

::: tip Why not just use Supabase's REST/JS SDK directly?
You can — but you'd lose Wave's input validation, RBAC, audit log,
rate limiting, and audit trail. Wave-in-front gives you a clean
HTTP surface and Postgres gives you the storage. Best of both.
:::

## What you need first

Pick any provider, copy the connection string:

| Provider | Free tier | Where to find DSN |
|---|---|---|
| **Supabase** | 500MB | Project Settings → Database → Connection string (Direct, not pooled) |
| **Neon** | 0.5GB | Dashboard → Connection Details → Pooled or direct |
| **Railway** | $5 trial | Postgres service → Connect tab |
| **Render** | 90-day free | Database → Connections |
| **Aiven** | 1-month trial | Service → Overview → Connection URI |
| **Crunchy Bridge** | $20+ | Cluster → URIs |

The string looks like:
```
postgresql://user:password@host.example.com:5432/dbname?sslmode=require
```

## YAML

```yaml
default:
  port: 8080

env:
  PG_DSN: { description: "Postgres connection string" }

plugins:
  pg:
    transport: longlived
    kind:      storage
    command:   ["/usr/local/bin/wave-postgres-storage"]
    timeout:   10s
    env:
      PG_DSN: "${env:PG_DSN}"

storage:
  app:
    type:   plugin               # backed by the pg plugin instead of SQLite
    plugin: pg
    # (No `tables:` block — manage schema with `wave migrate up` or
    # your provider's UI. Wave doesn't auto-create tables for plugin
    # backends.)

routes:
  - path: /users
    method: POST
    type: storage-access
    inputs:
      - { name: name,  source: body, type: string, required: true, min: 1, max: 200 }
      - { name: email, source: body, type: email,  required: true }
    storage-access:
      source: app
      # Postgres uses $1/$2/$3 placeholders, but Wave's {{name}}
      # template emits parameterised placeholders that the plugin
      # adapts to the driver's preferred form.
      execute: |
        INSERT INTO users(name, email) VALUES ({{name}}, {{email}})
        RETURNING id
      output_template: '{"id": {{.Data.id}}}'

  - path: /users/{id}
    method: GET
    type: storage-access
    inputs:
      - { name: id, source: path, type: int, required: true }
    storage-access:
      source: app
      execute: "SELECT id, name, email FROM users WHERE id = {{id}} LIMIT 1"
      if_empty_status: 404
      output_template: '{{toJSON .Data}}'

  - path: /users
    method: GET
    type: storage-access
    storage-access:
      source: app
      execute: "SELECT id, name, email FROM users ORDER BY id DESC LIMIT 100"
      output_template: '{{toJSON .Data}}'
```

## Build the plugin

If you've cloned the Wave repo, the postgres-storage plugin is
already in `examples/plugins/postgres-storage/`:

```sh
cd examples/plugins/postgres-storage
go build -o /usr/local/bin/wave-postgres-storage .
```

## Try it

```sh
export PG_DSN="postgresql://...@host:5432/dbname?sslmode=require"

# First time: create the schema. Either via wave migrate or directly:
psql "$PG_DSN" -c "CREATE TABLE IF NOT EXISTS users(id SERIAL PRIMARY KEY, name TEXT NOT NULL, email TEXT NOT NULL UNIQUE);"

wave serve server.yaml --port 8080

curl -X POST -d '{"name":"ada","email":"ada@example.com"}' http://localhost:8080/users
# {"id": 1}

curl http://localhost:8080/users
# [{"email":"ada@example.com","id":1,"name":"ada"}]
```

## Migrations

`wave migrate up --db <sqlite-only>` doesn't work against Postgres.
Three options that do:

| Tool | When to use |
|---|---|
| **Provider's web UI** | Quick prototypes — paste DDL into Supabase's SQL Editor |
| **`golang-migrate`** | Most common — file-based, multi-DB. `migrate -path ./migrations -database $PG_DSN up` |
| **dbmate / sqlx-migrate / Goose** | Same shape as above; pick by team preference |

Migrations live alongside `server.yaml` in `migrations/`:

```
0001_create_users.up.sql
0001_create_users.down.sql
0002_add_email_index.up.sql
0002_add_email_index.down.sql
```

## Connection pooling

Most managed Postgres providers have a **pooled** connection
endpoint (Supabase Supavisor on port 6543, Neon's pgbouncer on
`-pooler`). Use it in production — Wave's plugin opens one
connection per worker; the pooler multiplexes many of those onto a
shared pool of actual Postgres connections.

```
postgresql://...@aws-0-us-east-1.pooler.supabase.com:6543/postgres?pgbouncer=true
```

::: warning Transactions with poolers
PgBouncer **transaction mode** doesn't support prepared statements
or session-level `SET` — you can't use Wave's multi-statement SQL
across a pooled connection. For multi-statement transactions, use
the direct (non-pooled) connection.
:::

## Supabase-specific: use their auth + your routes

If you already use Supabase Auth on the frontend, you can verify
their JWTs in Wave and skip the auth setup:

```yaml
auth:
  supabase:
    type:   jwt
    secret: "${env:SUPABASE_JWT_SECRET}"   # Project Settings → JWT Secret
    cookie_name: sb-access-token

routes:
  - path: /me
    method: GET
    auth: [supabase]
    type: storage-access
    storage-access:
      source: app
      execute: "SELECT id, email FROM users WHERE id = {{getUser}}::int"
      output_template: '{{toJSON .Data}}'
```

`{{getUser}}` returns the `sub` claim from the Supabase JWT — same
shape as any other JWT auth.

## Production checklist

- [ ] Use the **pooled** connection in production
- [ ] **Pin the plugin binary** by SHA in your deploy — version drift in
      Postgres clients can introduce subtle bugs
- [ ] **Backup**: managed providers do daily snapshots by default;
      verify yours
- [ ] **Index every column you filter on** — Wave's strict-scope SQL
      makes EXPLAIN ANALYZE trivial (you know exactly which queries
      run)
- [ ] **Row-level security (RLS)** in Postgres — even if Wave's RBAC
      gates the route, RLS gives you defense-in-depth at the DB
- [ ] **Don't expose Postgres directly** to clients — let Wave be the
      front door

## Other plugin storage backends

The same shape works for any database with a plugin:

| Backend | Plugin |
|---|---|
| **PostgreSQL** | `postgres-storage` (in repo) |
| **MySQL / MariaDB** | Wrap `database/sql` — ~80 lines of Go from the postgres template |
| **MongoDB** | Wrap the Mongo driver |
| **DynamoDB** | Wrap the AWS SDK |
| **Redis / Valkey** | Wrap go-redis; use for KV via `storage.get`/`set`/`delete` |
| **ClickHouse** | Wrap clickhouse-go |
| **Anything with a Go driver** | Take the postgres plugin as a template; replace pgx with your driver |

See the [Build a plugin recipe](/cookbook/build-plugin) for the
storage-kind contract.

## See also

- [Storage concept](/guide/concepts-storage) — SQL helpers, pipelines, multi-statement
- [Plugin contract reference](/reference/plugin-contract) — storage-kind methods (get/set/delete/query/migrate)
- [Build a plugin recipe](/cookbook/build-plugin) — for porting storage plugins to other languages
- Demos: [`postgres-crud`](https://github.com/luowensheng/wave/tree/main/examples/apps/postgres-crud), [`postgres-plugin-crud`](https://github.com/luowensheng/wave/tree/main/examples/apps/postgres-plugin-crud)
- Plugin source: [`examples/plugins/postgres-storage`](https://github.com/luowensheng/wave/tree/main/examples/plugins/postgres-storage)
