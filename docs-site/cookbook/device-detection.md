# Device detection (mobile UA)

Serve different responses to mobile vs desktop clients. Detected
from the `User-Agent` header.

```yaml
routes:
  - path: /
    type: match
    match:
      cases:
        - when: header
          match:
            user-agent: { regex: "Mobile|Android|iPhone|iPad" }
          route:
            type: content
            content:
              status_code: 200
              headers: [["Content-Type", "text/html; charset=utf-8"]]
              body: |
                <!doctype html>
                <h1>Mobile homepage</h1>
                <a href="/app">Open app</a>

      default:
        route:
          type: content
          content:
            status_code: 200
            headers: [["Content-Type", "text/html; charset=utf-8"]]
            body: |
              <!doctype html>
              <h1>Desktop homepage</h1>
              <p>Visit on a phone for the mobile experience.</p>
```

## Try it

```sh
# Desktop
curl http://localhost:8080/
# Desktop homepage

# Simulate mobile
curl -H 'User-Agent: iPhone' http://localhost:8080/
# Mobile homepage
```

## Variations

- **Forward to a mobile backend**: replace the inline `content` route
  with a `forward` route pointing at a separate mobile-optimized
  service.
- **Use cookies as override**: add a higher-priority case that checks
  `cookie.force=mobile` to let QA force the mobile branch on desktop.
- **Per-device caching**: combine with per-route `cache:` so the
  mobile and desktop variants both get edge-cached separately.

## See also

- Demo: [`examples/apps/match-route-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/match-route-demo)
