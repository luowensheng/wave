# Quickstart

5 minutes from zero to a running API.

## 1. Install

::: code-group

```sh [Go]
go install github.com/luowensheng/wave/orchestrator@latest
# binary lands at $GOBIN/orchestrator — symlink as `wave`
ln -s "$(go env GOBIN || echo $HOME/go/bin)/orchestrator" /usr/local/bin/wave
```

```sh [Pre-built]
curl -sSfL https://luowensheng.github.io/wave/install.sh | sh
# Or pin a specific version:
# curl -sSfL https://luowensheng.github.io/wave/install.sh | sh -s -- v0.1.0
```

```sh [Docker]
docker run --rm -p 8080:8080 \
  -v $(pwd)/server.yaml:/server.yaml \
  ghcr.io/luowensheng/wave:latest serve /server.yaml --port 8080
```

:::

Verify:

```sh
wave version
```

## 2. Write your first server

Create `server.yaml`:

```yaml
default:
  port: 8080

storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      items:
        columns:
          - id         INTEGER PRIMARY KEY
          - name       TEXT NOT NULL
          - created_at TEXT NOT NULL DEFAULT (datetime('now'))

routes:
  # Create
  - path: /items
    method: POST
    type: storage-access
    inputs:
      - { name: name, source: body, type: string, required: true, min: 1 }
    storage-access:
      source: app
      execute: "INSERT INTO items(name) VALUES ({{name}})"
      output_template: '{"id": {{.LastInsertID}}}'

  # Read one
  - path: /items/{id}
    method: GET
    type: storage-access
    inputs:
      - { name: id, source: path, type: int, required: true }
    storage-access:
      source: app
      execute: "SELECT * FROM items WHERE id = {{id}} LIMIT 1"
      if_empty_status: 404
      output_template: '{{toJSON .Data}}'

  # List
  - path: /items
    method: GET
    type: storage-access
    storage-access:
      source: app
      execute: "SELECT * FROM items ORDER BY id DESC"
      output_template: '{{toJSON .Data}}'
```

## 3. Run it

```sh
wave serve server.yaml --port 8080
```

You'll see startup logs ending in:

```
Server starting http://127.0.0.1:8080
```

## 4. Hit it

```sh
# Create
curl -X POST -d '{"name":"first item"}' http://localhost:8080/items
# {"id": 1}

# Read
curl http://localhost:8080/items/1
# {"created_at":"...","id":1,"name":"first item"}

# List
curl http://localhost:8080/items
# [{"id":1,"name":"first item",...}]

# Missing item → 404
curl -i http://localhost:8080/items/999
# HTTP/1.1 404 Not Found
# {"error":"not found"}

# Built-in health
curl http://localhost:8080/healthz
# ok
```

## 5. What you just got

In 30 lines of YAML, Wave gave you:

- **Three routes** with HTTP method dispatch
- **Input validation** — POST without a name → 400 with field errors
- **SQL injection protection** — `{{name}}` becomes a `?` parameterised binding
- **404 handling** — `if_empty_status: 404` on GET-by-id
- **Health endpoint** at `/healthz`
- **JSON 404 envelope** for unmatched routes
- **Server-side migrations** — `tables:` ran the `CREATE TABLE` automatically

## Next steps

- [**Tutorial**](/guide/tutorial) — add auth, scheduling, and observability
- [**Cookbook**](/cookbook/) — copy-paste recipes for common patterns
- [**Reference**](/reference/) — every YAML key, every route type
