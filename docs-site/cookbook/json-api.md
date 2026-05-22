# JSON API with SQLite

A REST-style API persisted to SQLite. Covers create, read-one,
read-many, update, and delete with input validation and 404 handling.

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
          - id          INTEGER PRIMARY KEY AUTOINCREMENT
          - name        TEXT NOT NULL
          - description TEXT
          - created_at  TEXT NOT NULL DEFAULT (datetime('now'))
          - updated_at  TEXT NOT NULL DEFAULT (datetime('now'))

routes:
  # POST /items — create
  - path: /items
    method: POST
    type: storage-access
    inputs:
      - { name: name,        source: body, type: string, required: true, min: 1, max: 200 }
      - { name: description, source: body, type: string, required: false, max: 2000 }
    storage-access:
      source: app
      execute: |
        INSERT INTO items(name, description)
        VALUES ({{name}}, {{description}})
      response_content_type: application/json
      output_template: '{"id": {{.LastInsertID}}}'

  # GET /items/{id} — read one
  - path: /items/{id}
    method: GET
    type: storage-access
    inputs:
      - { name: id, source: path, type: int, required: true }
    storage-access:
      source: app
      execute: "SELECT * FROM items WHERE id = {{id}} LIMIT 1"
      if_empty_status: 404
      response_content_type: application/json
      output_template: '{{toJSON .Data}}'

  # GET /items — list (with optional ?q= filter)
  - path: /items
    method: GET
    type: storage-access
    inputs:
      - { name: q, source: query, type: string, required: false, max: 100 }
    storage-access:
      source: app
      execute: |
        SELECT * FROM items
        WHERE 1=1
        {{if hasvalue "q"}} AND name LIKE {{wrap "%q%"}} {{end}}
        ORDER BY id DESC
        LIMIT 100
      response_content_type: application/json
      output_template: '{{toJSON .Data}}'

  # PATCH /items/{id} — update
  - path: /items/{id}
    method: PATCH
    type: storage-access
    inputs:
      - { name: id,          source: path, type: int,    required: true }
      - { name: name,        source: body, type: string, required: false, max: 200 }
      - { name: description, source: body, type: string, required: false, max: 2000 }
    storage-access:
      source: app
      execute: |
        UPDATE items
        SET name        = COALESCE(NULLIF({{name}}, ''), name),
            description = COALESCE(NULLIF({{description}}, ''), description),
            updated_at  = {{getCurrentTime}}
        WHERE id = {{id}};
        SELECT * FROM items WHERE id = {{id}} LIMIT 1
      if_empty_status: 404
      response_content_type: application/json
      output_template: '{{toJSON .Data}}'

  # DELETE /items/{id}
  - path: /items/{id}
    method: DELETE
    type: storage-access
    inputs:
      - { name: id, source: path, type: int, required: true }
    storage-access:
      source: app
      execute: "DELETE FROM items WHERE id = {{id}}"
      response_content_type: application/json
      output_template: '{"deleted": {{.RowsAffected}}}'
```

## Try it

```sh
wave serve server.yaml --port 8080

# Create
curl -X POST -H 'Content-Type: application/json' \
     -d '{"name":"laptop","description":"silver, 16GB"}' \
     http://localhost:8080/items
# {"id": 1}

# Read
curl http://localhost:8080/items/1
# {"created_at":"...","description":"silver, 16GB","id":1,"name":"laptop",...}

# List with search
curl 'http://localhost:8080/items?q=lap'

# Update
curl -X PATCH -H 'Content-Type: application/json' \
     -d '{"description":"silver, 32GB"}' \
     http://localhost:8080/items/1

# Delete
curl -X DELETE http://localhost:8080/items/1
# {"deleted": 1}
```

## What's wired automatically

- **Input validation** — POST without a name → 400 with field errors
- **SQL injection protection** — `{{name}}` becomes a parameterised `?`
- **404 handling** — `if_empty_status: 404` returns `{"error":"not found"}`
- **Multi-statement SQL** — PATCH updates then selects in one block
- **Search helper** — `{{wrap "%q%"}}` for `LIKE %term%` patterns

## Next steps

- Add [auth](/cookbook/oauth) so only signed-in users can mutate
- Add an [audit log](/cookbook/audit-log) for every mutation
- Add a [rate limit](/cookbook/rate-limit) on POST/PATCH/DELETE
