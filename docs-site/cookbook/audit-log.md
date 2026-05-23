# Audit log every mutation

Persist a tamper-evident, append-only record of every state change
in your app. Useful for compliance (SOC2, HIPAA), debugging
production incidents, and answering "who did what when".

## YAML

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
          - id        INTEGER PRIMARY KEY AUTOINCREMENT
          - name      TEXT NOT NULL
          - price     REAL NOT NULL
          - updated_at TEXT NOT NULL DEFAULT (datetime('now'))

      audit_log:
        # Append-only — never UPDATE or DELETE rows in this table
        columns:
          - id        INTEGER PRIMARY KEY AUTOINCREMENT
          - actor     TEXT NOT NULL              # user id or 'system'
          - action    TEXT NOT NULL              # 'create', 'update', 'delete'
          - target    TEXT NOT NULL              # 'item:42'
          - before    TEXT                       # JSON snapshot before
          - after     TEXT                       # JSON snapshot after
          - ip        TEXT
          - at        TEXT NOT NULL DEFAULT (datetime('now'))

routes:
  # Update an item — log before + after atomically
  - path: /items/{id}
    method: PATCH
    auth: [app]
    type: storage-access
    inputs:
      - { name: id,    source: path, type: int,    required: true }
      - { name: price, source: body, type: float,  required: true, min: 0 }
    storage-access:
      source: app
      execute: |
        INSERT INTO audit_log(actor, action, target, before, after, ip)
          SELECT {{getUser}}, 'update', 'item:' || id,
                 json_object('id', id, 'name', name, 'price', price),
                 json_object('id', id, 'name', name, 'price', {{price}}),
                 {{getClientIP}}
          FROM items WHERE id = {{id}};
        UPDATE items SET price = {{price}}, updated_at = {{getCurrentTime}} WHERE id = {{id}};
        SELECT * FROM items WHERE id = {{id}} LIMIT 1
      if_empty_status: 404
      output_template: '{{toJSON .Data}}'

  # Admin: read the audit log
  - path: /admin/audit
    method: GET
    auth: [app]
    require_roles: [admin]
    type: storage-access
    inputs:
      - { name: target, source: query, type: string, required: false, max: 200 }
      - { name: actor,  source: query, type: string, required: false, max: 200 }
    storage-access:
      source: app
      execute: |
        SELECT * FROM audit_log
        WHERE 1=1
        {{if hasvalue "target"}} AND target = {{target}} {{end}}
        {{if hasvalue "actor"}}  AND actor  = {{actor}}  {{end}}
        ORDER BY id DESC
        LIMIT 200
      output_template: '{{toJSON .Data}}'
```

## What's wired automatically

- **`{{getUser}}` template helper** — pulls the authenticated user's
  id from the request context. Available in any route with `auth:`
  set.
- **`{{getClientIP}}` helper** — best-guess client IP from
  X-Forwarded-For / X-Real-IP / RemoteAddr.
- **`{{getCurrentTime}}` helper** — server-side timestamp, never
  trusts the client clock.
- **Multi-statement SQL** — the audit insert and the actual update
  run in a single transaction. If the update fails, the audit row
  is rolled back too.

## Tamper-evidence

Pure append-only isn't quite tamper-proof — a DB admin could `DELETE
FROM audit_log`. To harden:

1. **Hash chain**: add a `prev_hash` column, fill with
   `sha256(prev_hash + this_row_json)`. Breaks under deletion.
2. **External sink**: also publish each audit row to an
   [outbox](/cookbook/outbox) → external SIEM. Comparing the two
   surfaces tampering.
3. **Read-only replica**: stream the audit table to a separate
   storage backend that the application can't write to.

## Compliance notes

For SOC2 you'll typically need:
- Who: actor (user id or service principal)
- What: action + target + before/after
- When: server-side timestamp
- Where: source IP
- Why: optional, usually a "reason" form field

This recipe covers the first four; add `reason` to `inputs:` if
your auditors want the fifth.

## See also

- Demo: [`audit-logged-admin`](https://github.com/luowensheng/wave/tree/main/examples/apps/audit-logged-admin)
- [Outbox-backed delivery](/cookbook/outbox) for shipping audit rows
  to a SIEM / long-term storage.
