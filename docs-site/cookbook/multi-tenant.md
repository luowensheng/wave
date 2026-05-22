# Multi-tenant by Host header

You operate one Wave server that handles requests for multiple
tenants, each on their own subdomain. Route incoming requests to
different backends or storage based on the `Host` header.

```yaml
storage:
  acme_db:
    type: sqlite
    path: ./tenants/acme.db
  globex_db:
    type: sqlite
    path: ./tenants/globex.db

routes:
  - path: /users
    method: GET
    type: match
    match:
      cases:
        - when: host
          match: acme.example.com
          route:
            type: storage-access
            storage-access:
              source: acme_db
              execute: "SELECT id, name FROM users ORDER BY id"
              output_template: '{{toJSON .Data}}'

        - when: host
          match: globex.example.com
          route:
            type: storage-access
            storage-access:
              source: globex_db
              execute: "SELECT id, name FROM users ORDER BY id"
              output_template: '{{toJSON .Data}}'

      default:
        route:
          type: content
          content:
            status_code: 404
            headers: [["Content-Type", "application/json"]]
            body: '{"error":"unknown tenant"}'
```

## Try it

```sh
wave serve server.yaml --port 8080

curl -H 'Host: acme.example.com' http://localhost:8080/users
# returns Acme's users

curl -H 'Host: globex.example.com' http://localhost:8080/users
# returns Globex's users

curl -H 'Host: unknown.example.com' http://localhost:8080/users
# {"error":"unknown tenant"}
```

## Variations

- **Regex over subdomain**: `match: { regex: ".*\\.acme\\.com$" }`
  to match any subdomain of acme.com.
- **Combine with auth**: each tenant's case can have its own `auth:`
  field — Acme uses Google OIDC, Globex uses SAML.
- **Per-tenant rate limit**: each case can reference a different
  entry in `limits:` so quotas don't cross tenants.

::: tip
For more tenants, factor each tenant's handler into a library-only
route with an `id`, then reference it from match cases:
`route: acme_users_handler`. Keeps the YAML compact.
:::

## See also

- [Routes](/guide/concepts-routes) — full `type: match` reference
- Demo: [`examples/apps/match-route-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/match-route-demo)
