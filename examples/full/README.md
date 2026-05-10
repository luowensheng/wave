# Full reference example

A single `server.yaml` exercising most of wave's surface in one
place — useful as a tour or as a copy-paste starting point.

What it shows:

- **`type: process`** preserved for shell-style instant APIs (`/shell/upcase`)
- **`type: plugin`** with retries, response filtering, per-route cache, circuit breaker, rate limit (`/plugins/echo/{id}`)
- **`type: stream-publish`** with HMAC verification, field filtering, forward-url with outbound HMAC signing (`/webhooks/stripe`)
- **`type: forward`** with templated `{id}` path params (`/things/{id}`)
- **`type: graphql`** dispatching to a plugin (`/graphql`)
- **`type: file`** behind `forward_auth` middleware delegating to an external auth service (`/private/protected`)
- **`type: api`** with inline JSON-Schema request body validation (`/api/orders`)
- **`connections:`** with `auto` (SSE+WS upgrade-aware) and `ws` brokers
- **Durable outbox** + **scheduled jobs** at the top level
- **Auto-served operational endpoints**: `/healthz` `/readyz` `/version` `/metrics` `/admin/` `/docs` `/openapi.json` `/api/streams.json`

## Run

```sh
STRIPE_WEBHOOK_SECRET=whsec_test \
FWDAUTH_URL=https://example.com/api/verify \
wave serve examples/full/server.yaml
```

Then:

```sh
# Operational
curl -s http://localhost:8080/healthz
curl -s http://localhost:8080/metrics | head
curl -s http://localhost:8080/openapi.json | jq '.paths | keys'
open http://localhost:8080/admin/
open http://localhost:8080/docs

# Inspect the route table without booting
wave routes ./server.yaml
wave validate ./server.yaml
wave doctor   ./server.yaml

# Outbox introspection (after some webhook traffic)
wave outbox list   --db ./outbox.db
wave outbox dlq    --db ./outbox.db
wave outbox replay --db ./outbox.db --all
```
