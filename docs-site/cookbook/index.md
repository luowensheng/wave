# Cookbook

Task-oriented recipes. Each one is self-contained and runnable.
Most have a matching runnable demo in
[`examples/apps/`](https://github.com/luowensheng/wave/tree/main/examples/apps).

## Backend basics

- [**JSON API with SQLite**](/cookbook/json-api) — CRUD, validation, 404, search
- [**File uploads & downloads**](/cookbook/file-uploads) — multipart form, served binary
- [**Rate-limit an endpoint**](/cookbook/rate-limit) — token bucket, by IP or user claim
- [**Functional testing (`wave test`)**](/cookbook/testing) — YAML test suites, in-process server, CI-ready
- [**Build a plugin (any language)**](/cookbook/build-plugin) — same echo plugin in Go, Python, Node, Rust, Bash

## Auth

- [**Magic-link login**](/cookbook/magic-link-login) — passwordless email flow
- [**OAuth with Google/GitHub/Apple**](/cookbook/oauth) — OIDC and OAuth2 patterns
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

## Integrations

- [**Forward Stripe webhooks**](/cookbook/stripe-webhooks) — HMAC verify, persist, fan-out
- [**Outbox-backed delivery**](/cookbook/outbox) — durable webhooks with retry + DLQ

## Use Wave alongside your existing stack

- [**React / Next.js + Wave backend**](/cookbook/react-wave) — full integration with cookies, CORS, deploy
- [**Wave in front of a Node service**](/cookbook/node-gateway) — gateway pattern: auth/rate-limit/audit without touching Node
- [**Wave as a Python sidekick**](/cookbook/python-sidekick) — wrap a FastAPI / ML service with auth + SSE + tasks
- [**Migrating from Express incrementally**](/cookbook/migrate-from-express) — route-at-a-time path, no big-bang

::: tip Browse more demos
[`examples/apps/INDEX.md`](https://github.com/luowensheng/wave/blob/main/examples/apps/INDEX.md)
lists all 64 runnable demos — including patterns not yet covered as
cookbook recipes (chat-room, polls, photo-gallery, multi-plugin-stack,
SAML SSO, vault-secrets, OTel tracing, and more).
:::

::: info Missing a recipe?
File a [feature request](https://github.com/luowensheng/wave/issues/new/choose)
or open a [Discussion](https://github.com/luowensheng/wave/discussions).
:::
