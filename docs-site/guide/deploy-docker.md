# Deploy with Docker

## The official image

```sh
docker run --rm -p 8080:8080 \
  -v $(pwd)/server.yaml:/app/server.yaml \
  ghcr.io/luowensheng/wave:latest \
  serve /app/server.yaml --port 8080 --host 0.0.0.0
```

- Distroless nonroot base (~25 MB)
- CGO-enabled, so the built-in SQLite backend works
- Image tags: `latest`, `vX.Y.Z`, `vX.Y.Z-amd64`, `vX.Y.Z-arm64`
- Multi-arch: amd64 + arm64 picked automatically by Docker

## docker-compose

```yaml
# docker-compose.yml
services:
  wave:
    image: ghcr.io/luowensheng/wave:latest
    restart: unless-stopped
    ports:
      - "8080:8080"
    volumes:
      - ./server.yaml:/app/server.yaml:ro
      - data:/app/data
    command: serve /app/server.yaml --port 8080 --host 0.0.0.0
    environment:
      JWT_SECRET: change-me-in-prod
    healthcheck:
      test: ["CMD", "/usr/local/bin/wave", "version"]
      interval: 30s
      timeout: 3s
      retries: 3

volumes:
  data:
```

```sh
docker-compose up -d
docker-compose logs -f wave
```

## Custom image (build from your repo)

If your `server.yaml` lives in your own repo, bake it into a slim
derived image:

```dockerfile
FROM ghcr.io/luowensheng/wave:latest
COPY server.yaml /app/server.yaml
COPY assets /app/assets
CMD ["serve", "/app/server.yaml", "--port", "8080", "--host", "0.0.0.0"]
```

## Volumes you'll typically want

| Mount point | Purpose |
|---|---|
| `/app/server.yaml` | The config (read-only) |
| `/app/data` | SQLite DB files (persistent) |
| `/app/outbox.db` | Outbox DB if `outbox_db:` is set |
| `/app/assets` | Static files, templates |

Mount `/app/data` to a host volume or a managed volume — losing
the SQLite file means losing your data.

## Pinning a version

```sh
docker run ghcr.io/luowensheng/wave:v0.1.0 ...
```

Pinning is recommended for production. Wave is pre-1.0; reading the
[CHANGELOG](https://github.com/luowensheng/wave/blob/main/CHANGELOG.md)
before bumping is the contract.

## Security notes

- The image runs as **UID 65532** (nonroot). Mount volumes with
  matching ownership.
- Distroless contains **no shell** — `docker exec wave sh` won't
  work. Debug with a sidecar or by switching tags to `:debug` if
  ever published.
- The image **does not phone home**. See the [Privacy page](/guide/privacy).

## Verifying the image

Image signatures land alongside binaries via sigstore keyless OIDC:

```sh
cosign verify ghcr.io/luowensheng/wave:v0.1.0 \
  --certificate-identity-regexp='https://github.com/luowensheng/wave/' \
  --certificate-oidc-issuer='https://token.actions.githubusercontent.com'
```

## See also

- [Fly.io deploy](/guide/deploy-fly)
- [Production checklist](/guide/deploy-checklist)
- [Observability](/guide/concepts-observability) — wire `/metrics`
  scraping
