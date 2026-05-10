# Storage plugins

Routes of `type: storage-access` resolve their `source:` against two
namespaces, tried in order:

1. Built-in storage backends declared under the top-level `storage:` block
   (`sqlite`, `filesystem`, …).
2. `kind: storage` plugins declared under the top-level `plugins:` block.

This means an existing config keeps working unchanged; you only need to
add a plugin when you want a backend the orchestrator doesn't ship.

## Declaring a storage plugin

```yaml
plugins:
  pg_main:
    kind: storage
    transport: process
    command: ./wave-postgres
    env:
      PG_DSN: "${ENV:PG_DSN}"
      PG_MAX_CONNS: "10"
```

The plugin process is started lazily on first use and kept alive across
requests; restart-on-exit is handled by the long-lived JSON-RPC
transport.

## Referencing the plugin from a route

```yaml
routes:
  - path: /api/items
    type: storage-access
    method: GET
    storage-access:
      source: pg_main          # name resolves to the plugin above
      execute: "SELECT * FROM items WHERE owner = '{{.user_id}}'"
      output_template: "{{toJSON .}}"
      response_content_type: application/json
```

Built-in storage names continue to work alongside plugin sources:

```yaml
storage:
  cache:
    type: sqlite
    path: ./cache.db

routes:
  - path: /api/cache
    type: storage-access
    storage-access:
      source: cache            # built-in, takes precedence
      execute: "SELECT * FROM kv"
      output_template: "{{toJSON .}}"
```

## Phase 2 caveats

- Templates substitute via `text/template`; parameter binding (`?`,
  `$1`) is not yet exposed to plugins. Quote dynamic values inside the
  template until named-param support lands.
- Boot fails fast if a route's `source` resolves to neither a built-in
  nor a plugin.
