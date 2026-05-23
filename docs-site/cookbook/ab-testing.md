# A/B testing via cookie

Run two variants of a page against different users. Variant is
decided by a cookie set on first visit and sticky thereafter.

## YAML

```yaml
default:
  port: 8080

routes:
  - path: /
    type: match
    match:
      cases:
        - when: cookie
          match: { variant: beta }
          route:
            type: content
            content:
              status_code: 200
              headers: [["Content-Type", "text/html; charset=utf-8"]]
              body: |
                <!doctype html>
                <h1>Beta homepage</h1>
                <p>Welcome to the new experience.</p>

        - when: cookie
          match: { variant: control }
          route:
            type: content
            content:
              status_code: 200
              headers: [["Content-Type", "text/html; charset=utf-8"]]
              body: |
                <!doctype html>
                <h1>Classic homepage</h1>

      # First-time visitor — assign a variant (50/50) and reload
      default:
        route:
          type: process
          process:
            script: |
              # Simple coinflip; in production you'd seed with a hash of
              # the user's IP so they always get the same variant.
              variant=$([ $((RANDOM % 2)) -eq 0 ] && echo "beta" || echo "control")
              echo "Set-Cookie: variant=$variant; Path=/; Max-Age=2592000; HttpOnly; SameSite=Lax"
              echo "Location: /"
              echo "Status: 302"
```

## Try it

```sh
wave serve server.yaml --port 8080

# First visit — gets assigned to a variant, redirected
curl -i -c cookies.txt http://localhost:8080/

# Subsequent visits stay on the assigned variant
curl -b cookies.txt http://localhost:8080/
```

## Cleaner version with a `set-variant` route

The coinflip-then-redirect-via-process pattern is ugly. Cleaner:
have a dedicated `set-variant` route that the first-visit default
redirects to.

```yaml
routes:
  - path: /
    type: match
    match:
      cases:
        - when: cookie
          match: { variant: { exists: true } }
          route: home_page                  # by-id reference
      default:
        route:
          type: redirect
          redirect:
            redirect_url: http://localhost:8080/_set-variant?next=/
            status_code: 302

  - path: /_set-variant
    method: GET
    type: storage-access
    inputs:
      - { name: next, source: query, type: string, required: false }
    storage-access:
      source: app
      # log the assignment for analytics, then set the cookie via headers
      execute: "INSERT INTO assignments(variant) VALUES (CASE WHEN abs(random()) % 2 = 0 THEN 'beta' ELSE 'control' END) RETURNING variant"
      response_content_type: text/html
      output_template: |
        <!doctype html><script>
          document.cookie = 'variant={{.Data.variant}}; path=/; max-age=2592000';
          location = '{{.next}}' || '/';
        </script>

  # Library-only — the actual home page (no path)
  - id: home_page
    type: match
    match:
      cases:
        - when: cookie
          match: { variant: beta }
          route: { type: content, content: { body: "Beta homepage" } }
      default:
        route: { type: content, content: { body: "Classic homepage" } }
```

## Variations

- **Sticky by user, not by browser**: bucket on a hash of the user
  ID so the same user gets the same variant across devices.
- **Three-way test**: add a third case (e.g., `gamma`).
- **Server-pushed assignment**: assign via your auth backend at
  signup, store on the user record, read into a JWT claim.
- **Combine with feature flags**: a higher-priority case checks a
  header like `x-force-variant` so QA can pin a variant on demand.

## Measurement

Each variant is its own route. Add a [Prometheus counter](/guide/concepts-observability)
per variant, or pipe the assignment to your analytics endpoint via
an [outbox](/cookbook/outbox).

## See also

- [Device detection](/cookbook/device-detection) — same `type: match`
  primitive, different predicate.
- [Multi-tenant by Host header](/cookbook/multi-tenant)
- Demo: [`match-route-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/match-route-demo)
