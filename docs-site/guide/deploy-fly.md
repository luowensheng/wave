# Deploy to Fly.io

[Fly.io](https://fly.io) is a good fit for Wave: single binary,
SQLite-on-disk via Fly Volumes, and global anycast on a free tier.

## Prerequisites

- [`flyctl`](https://fly.io/docs/hands-on/install-flyctl/) installed and
  logged in (`fly auth login`)
- A Wave `server.yaml` in your project
- Optionally a Dockerfile (Fly can build one for you with `fly launch`)

## `fly.toml`

```toml
app = "my-wave-app"
primary_region = "iad"

[build]
  image = "ghcr.io/luowensheng/wave:latest"

[env]
  PORT = "8080"
  # JWT_SECRET, OAuth creds, etc. → fly secrets set, NOT here.

[[mounts]]
  source = "wave_data"
  destination = "/app/data"

[[services]]
  internal_port = 8080
  protocol = "tcp"

  [[services.ports]]
    port = 80
    handlers = ["http"]
    force_https = true
  [[services.ports]]
    port = 443
    handlers = ["tls", "http"]

  [services.concurrency]
    type = "requests"
    hard_limit = 250
    soft_limit = 200

  [[services.tcp_checks]]
    interval = "15s"
    timeout = "2s"

  [[services.http_checks]]
    interval = "10s"
    method = "get"
    path = "/healthz"
    protocol = "http"
    timeout = "2s"

[deploy]
  release_command = "wave validate /app/server.yaml"

[processes]
  app = "serve /app/server.yaml --port 8080 --host 0.0.0.0"
```

## Initial deploy

```sh
# 1. Set secrets (these become env vars in the container)
fly secrets set JWT_SECRET=$(openssl rand -hex 32) \
                GOOGLE_CLIENT_ID=... \
                GOOGLE_CLIENT_SECRET=...

# 2. Create the volume (one-time)
fly volumes create wave_data --region iad --size 1

# 3. Launch
fly deploy
```

Fly returns a URL like `https://my-wave-app.fly.dev`. Visit
`/healthz` to confirm.

## Bake your server.yaml into the image

The simplest path is a custom Dockerfile that bakes the config in:

```dockerfile
# Dockerfile
FROM ghcr.io/luowensheng/wave:latest
COPY server.yaml /app/server.yaml
COPY assets /app/assets
```

Update `fly.toml`:

```toml
[build]
  dockerfile = "Dockerfile"
```

Then `fly deploy`. The release command `wave validate /app/server.yaml`
will fail-fast if you ship a broken config.

## Persistence

SQLite + a Fly volume = stateful in-region storage at low cost.
For multi-region writes, see Fly's
[LiteFS](https://fly.io/docs/litefs/) (SQLite replication) or move
to a managed Postgres.

## Custom domain

```sh
fly certs create example.com
# Add the A/AAAA records Fly tells you to
```

## Updating

```sh
fly deploy                          # rebuild + roll
fly logs -a my-wave-app             # tail
fly status                          # health
fly ssh console                     # interactive shell into a VM
```

## Cost

A Wave app with one VM + 1 GB volume costs ~$3-5/month on Fly.
Many small projects fit in the free tier. See
[fly.io/docs/about/pricing](https://fly.io/docs/about/pricing/).

## See also

- [Docker deploy](/guide/deploy-docker)
- [Production checklist](/guide/deploy-checklist)
- [Observability](/guide/concepts-observability) — wire
  `/metrics` to Fly's Prometheus integration
