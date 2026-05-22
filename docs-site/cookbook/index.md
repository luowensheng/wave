# Cookbook

Task-oriented recipes. Each one is self-contained and runnable.

## Currently shipped

- [**JSON API with SQLite**](/cookbook/json-api) — CRUD, validation, 404 handling
- [**Multi-tenant by Host header**](/cookbook/multi-tenant) — `type: match` over host
- [**Device detection (mobile UA)**](/cookbook/device-detection) — UA regex dispatch
- [**CORS for a method-bound route**](/cookbook/cors-preflight) — make browser preflight work

## Coming soon

These recipes have working demos in [`examples/apps/`](https://github.com/luowensheng/wave/tree/main/examples/apps)
already; full cookbook writeups land in the v0.2 docs cycle.

- Magic-link login → see [`magic-link-login`](https://github.com/luowensheng/wave/tree/main/examples/apps/magic-link-login)
- OAuth (Google/GitHub) → see [`oauth-google-callback`](https://github.com/luowensheng/wave/tree/main/examples/apps) demos
- Stream events with SSE → see [`sse-chat`](https://github.com/luowensheng/wave/tree/main/examples/apps/sse-chat)
- Background tasks → see [`background-task-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/background-task-demo)
- Schedule a cron job → see [`scheduled-jobs-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/scheduled-jobs-demo)
- Forward Stripe webhooks → see [`stripe-webhooks-relay`](https://github.com/luowensheng/wave/tree/main/examples/apps)
- Rate-limit an endpoint → see [`rate-limited-public-api`](https://github.com/luowensheng/wave/tree/main/examples/apps/rate-limited-public-api)
- File uploads → see [`file-uploads`](https://github.com/luowensheng/wave/tree/main/examples/apps/file-uploads)
- Outbox-backed delivery → see [`outbox-reliability-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/outbox-reliability-demo)
- Audit log → see [`audit-logged-admin`](https://github.com/luowensheng/wave/tree/main/examples/apps/audit-logged-admin)

::: tip Don't see what you need?
File a [feature request](https://github.com/luowensheng/wave/issues/new/choose)
or ask in [Discussions](https://github.com/luowensheng/wave/discussions).
:::
