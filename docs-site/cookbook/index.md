# Cookbook

Task-oriented recipes — every page is something concrete you can
copy, paste, and ship.

::: tip Looking for the full feature list?
The cookbook shows you **how to do common things**. For an exhaustive
"what does Wave actually support?" surface, see the
[**full feature inventory**](/reference/features).
:::

---

## Backend basics

Start here if you're new — these cover ~80% of what most backends do.

- [**JSON API with SQLite**](/cookbook/json-api) — CRUD, validation, 404, search
- [**File uploads & downloads**](/cookbook/file-uploads) — multipart form, served binary
- [**Rate-limit an endpoint**](/cookbook/rate-limit) — token bucket, by IP or user claim
- [**Functional testing (`wave test`)**](/cookbook/testing) — YAML test suites, in-process server, CI-ready

## Auth

- [**Sign in with Google**](/cookbook/google-signin) — OIDC flow, domain-gated RBAC, Workspace groups
- [**OAuth with GitHub / Apple / generic**](/cookbook/oauth) — covers any OAuth 2.0 / OIDC provider
- [**Magic-link login**](/cookbook/magic-link-login) — passwordless email
- [**Audit log every mutation**](/cookbook/audit-log) — append-only mutation trail

## Routing

- [**Multi-tenant by Host header**](/cookbook/multi-tenant) — `type: match` over host
- [**Device detection (mobile UA)**](/cookbook/device-detection) — UA regex dispatch
- [**A/B testing via cookie**](/cookbook/ab-testing) — variant-cookie split
- [**CORS for a method-bound route**](/cookbook/cors-preflight) — fix preflight 405s

## Streaming & jobs

- [**Stream events with SSE**](/cookbook/sse) — server-sent events with replay buffer
- [**Background tasks**](/cookbook/background-tasks) — 202 + SSE progress, plugin-backed
- [**Schedule a cron job**](/cookbook/schedule) — every-N / daily-at, with sinks

## Reliability

- [**Forward Stripe webhooks**](/cookbook/stripe-webhooks) — HMAC verify, persist, fan-out
- [**Outbox-backed delivery**](/cookbook/outbox) — durable webhooks with retry + DLQ

## Third-party integrations

The most-asked "how do I connect Wave to X?" recipes. All use the same
small set of Wave primitives (`type: fetch`, `type: api`, `type: plugin`,
`webhook_sig:`, `requests:`) so once you've done one, the others are
copy-paste.

| Integration | Pattern | Recipe |
|---|---|---|
| **Sign in with Google** | OIDC | [Recipe](/cookbook/google-signin) |
| **Transactional email** (Resend / SendGrid / Postmark / Mailgun) | `type: fetch` | [Recipe](/cookbook/send-email) |
| **Stripe Checkout + Customer Portal** | `type: fetch` + `webhook_sig:` | [Recipe](/cookbook/stripe-checkout) |
| **Stripe webhooks** | `webhook_sig: stripe` | [Recipe](/cookbook/stripe-webhooks) |
| **OpenAI / Claude / Ollama chat** | `type: task` + `longlived` plugin | [Recipe](/cookbook/openai-claude) |
| **SMS via Twilio / Vonage / MessageBird** | `type: fetch` | [Recipe](/cookbook/twilio-sms) |
| **Slack slash command** | `webhook_sig: slack` | [Recipe](/cookbook/slack-slash-command) |
| **Image upload to S3 / R2 / B2 / Spaces** | Pre-signed PUT via plugin | [Recipe](/cookbook/s3-r2-uploads) |
| **Push notifications via FCM** | `type: fetch` + service-account plugin | [Recipe](/cookbook/firebase-fcm) |
| **Supabase / Neon / Railway Postgres** | `postgres-storage` plugin | [Recipe](/cookbook/supabase-postgres) |

::: tip Same shape, different provider
Most third-party APIs take JSON-over-HTTPS with an API key in a
header. Wave's [`type: fetch`](/cookbook/send-email) handles them in
~15 lines of YAML. If you can write a `curl` command for the API,
you can wire it into Wave.
:::

## Extending Wave

When the built-in primitives aren't enough — build your own.

- [**Build a plugin (any language)**](/cookbook/build-plugin) — the same echo plugin in Go, Python, Node, Rust, Bash. Worked examples for handler / storage / auth / secrets / exporter kinds.

---

## Use Wave alongside your existing stack

Wave isn't a replacement for React, Node, or Python. It slots in
alongside them.

- [**React / Next.js + Wave backend**](/cookbook/react-wave) — auth + persistence in YAML, frontend stays on Vercel
- [**Wave in front of a Node service**](/cookbook/node-gateway) — gateway pattern for auth, rate-limit, audit, webhook signatures
- [**Wave as a Python sidekick**](/cookbook/python-sidekick) — wrap a FastAPI / ML service with auth + SSE + tasks
- [**Migrating from Express incrementally**](/cookbook/migrate-from-express) — route-at-a-time, no big-bang

---

::: info Missing a recipe?
File a [feature request](https://github.com/luowensheng/wave/issues/new/choose)
or open a [Discussion](https://github.com/luowensheng/wave/discussions).
For the canonical list of demos not yet covered as cookbook recipes,
see [`examples/apps/INDEX.md`](https://github.com/luowensheng/wave/blob/main/examples/apps/INDEX.md)
(64 runnable apps).
:::
