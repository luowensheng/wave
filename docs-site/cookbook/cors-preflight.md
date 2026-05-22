# CORS for a method-bound route

Browser CORS preflights (`OPTIONS` requests) fail silently if you
bind a route with `method:` (singular) — Go's ServeMux 405s OPTIONS
before Wave's CORS wrapper runs.

## The footgun

```yaml
# ❌ BROKEN — OPTIONS preflights get 405 from the mux
- path: /api/query
  method: post              # singular method binds the pattern
  type: forward
  cors_origins: ["*"]
  forward: { forward_url: "http://backend:9000/api/query" }
```

## The fix

Use `methods:` (plural). With `method:` empty, the mux pattern is
just the path — OPTIONS reaches the CORS wrapper and gets answered.

```yaml
# ✅ WORKS
- path: /api/query
  methods: [POST]           # plural — pattern is method-less
  type: forward
  cors_origins: ["*"]
  forward: { forward_url: "http://backend:9000/api/query" }
```

## Verify

```sh
# Real POST works and gets CORS headers
curl -i -X POST -H 'Origin: https://app.example.com' http://localhost:8080/api/query
# HTTP/1.1 200 OK
# Access-Control-Allow-Origin: *

# Browser preflight succeeds (was 405 with `method: post`)
curl -i -X OPTIONS \
  -H 'Origin: https://app.example.com' \
  -H 'Access-Control-Request-Method: POST' \
  http://localhost:8080/api/query
# HTTP/1.1 204 No Content
# Access-Control-Allow-Methods: POST, OPTIONS  (reflected)

# Verb still gated correctly
curl -i -X DELETE http://localhost:8080/api/query
# HTTP/1.1 405 Method Not Allowed
```

## Variations

- **Whitelist specific origins** instead of `*`:

  ```yaml
  cors_origins:
    - "https://app.example.com"
    - "https://staging.example.com"
  ```

  Wave matches case-sensitively against the `Origin` header.

- **Per-handler CORS** (different rules per match case): each match
  case is its own route, so each can declare its own `cors_origins`.

## See also

- Demo: [`examples/apps/cors-preflight-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/cors-preflight-demo)
