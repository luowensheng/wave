# postgres-storage

Reference `kind: storage` plugin backed by PostgreSQL via `pgx/v5`. Talks
JSON-RPC over stdin/stdout; the wave long-lived plugin transport
spawns it lazily and restarts it on exit.

## Build

```sh
cd examples/plugins/postgres-storage
go build -o ./wave-postgres .
```

## Wire into a server

```yaml
plugins:
  pg_main:
    kind: storage
    transport: process
    command: ./wave-postgres
    env:
      PG_DSN: "${ENV:PG_DSN}"
      PG_MAX_CONNS: "10"

routes:
  - path: /api/items
    type: storage-access
    storage-access:
      source: pg_main
      execute: "SELECT * FROM items"
      output_template: "{{toJSON .}}"
      response_content_type: application/json
```

## Tests

Unit tests are gated behind a build tag and only run when `PG_TEST_DSN`
is set:

```sh
PG_TEST_DSN=postgres://localhost/test go test -tags=pgintegration ./...
```

CI does not run them.
