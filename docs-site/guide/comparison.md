# Comparison vs alternatives

Honest comparisons. Where Wave is worse, the table says so.

## Wave vs other Go frameworks

|  | Wave | Gin / Echo / Chi | Caddy |
|---|---|---|---|
| Configuration style | YAML, declarative | Go code | Caddyfile / JSON |
| Single binary | ✅ | ✅ (you build it) | ✅ |
| Built-in storage | ✅ SQLite + plugins | ❌ (you wire it) | ❌ |
| Built-in auth | ✅ (magic link, OAuth, TOTP, RBAC) | ❌ (libraries) | ✅ (caddy-security) |
| Webhook signature verification | ✅ Stripe/GitHub/Slack | ❌ | ⚠️ via plugin |
| Multi-statement SQL | ✅ | n/a | n/a |
| Plugin model | Out-of-process | In-process | In-process |
| Hot-reload config | ❌ | ❌ | ✅ |
| Lowest learning curve | YAML | Go | Caddyfile |
| Best for custom logic | ⚠️ via plugin | ✅ | ⚠️ via module |

**Pick Wave** when your domain is mostly CRUD + auth + webhooks and
you'd rather configure than code. **Pick Gin/Echo/Chi** when you
need deep customization of request handling. **Pick Caddy** if your
primary need is reverse proxy + TLS automation.

## Wave vs Node/Python frameworks

|  | Wave | Express / Fastify | FastAPI |
|---|---|---|---|
| Runtime | Single binary | Node.js | Python |
| Startup time | ~100ms | ~500ms | ~1-2s |
| Memory baseline | ~30 MB | ~80 MB | ~80 MB |
| Type safety | YAML schema | TypeScript optional | Pydantic |
| Ecosystem | Pre-1.0 | Massive | Large |
| Deployment | Copy binary | `npm install` + node | `pip install` + python |
| Best for prototyping | Tied | Tied | Tied |
| Best for OPS simplicity | ✅ | ❌ | ❌ |

**Pick Wave** when ops simplicity matters (no language runtime to
patch). **Pick Express/Fastify** when you have a Node team and need
mature middleware. **Pick FastAPI** when you need Pydantic-level
schema validation in Python.

## Wave vs Hasura / Supabase / PocketBase

|  | Wave | Hasura | Supabase | PocketBase |
|---|---|---|---|---|
| API style | REST + SSE (you author) | GraphQL (auto-generated) | REST + GraphQL (auto) | REST (auto) |
| Custom logic | Plugins | Actions / Triggers | Edge Functions | Hooks (JS) |
| Self-hosted | ✅ | ✅ | ✅ | ✅ |
| Auth built-in | ✅ | ⚠️ via JWT/external | ✅ | ✅ |
| YAML config | ✅ | ⚠️ metadata files | ❌ | ❌ |
| Schema-first | ❌ | ✅ | ✅ | ✅ |
| Best for "give me an API from a DB" | ❌ | ✅ | ✅ | ✅ |
| Best for "I want to handcraft the surface" | ✅ | ❌ | ⚠️ | ⚠️ |

**Pick Hasura/Supabase/PocketBase** if you want the API auto-generated
from a schema. **Pick Wave** if you want to author each endpoint
explicitly (CRUD + custom behavior + auth + integration in one config).

## Wave vs Kubernetes Ingress

|  | Wave | nginx-ingress / Traefik |
|---|---|---|
| Layer | Application | Edge / L7 routing |
| Stateful | ✅ (storage, sessions) | ❌ |
| Routes are application code? | Yes | No |

Different tools. You typically run Wave **behind** an Ingress, not
instead of one.

## When NOT to pick Wave

- **You have a Go team that already uses Gin/Echo/Chi.** Wave's YAML
  abstraction doesn't help you; raw Go probably does.
- **You need GraphQL as the primary API.** Wave has `type: graphql`
  but it's not its sweet spot.
- **You're building real-time games / streaming media.** Use a
  WebSocket-first framework or a custom net stack.
- **You need 100,000 rps single-node.** Wave hasn't been
  benchmarked at that scale. Profile first.
- **You can't afford pre-1.0.** Wave is < 1.0; breaking changes
  between 0.x minors are allowed. Pin and read CHANGELOGs.
