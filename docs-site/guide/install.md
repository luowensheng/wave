# Install

## Pre-built binaries

::: warning Pending v0.1.0 release
Pre-built binaries land on GitHub Releases at v0.1.0. Until then,
use the `go install` path below.
:::

```sh
# macOS / Linux — auto-detects arch and OS
curl -sSfL https://wave.dev/install.sh | sh

# Or download a specific tag
curl -sSfL https://github.com/luowensheng/wave/releases/download/v0.1.0/wave-darwin-arm64.tar.gz \
  | tar -xz -C /usr/local/bin
```

## Homebrew

```sh
brew install luowensheng/tap/wave
```

## Docker

```sh
docker run --rm -p 8080:8080 \
  -v $(pwd)/server.yaml:/server.yaml \
  ghcr.io/luowensheng/wave:latest serve /server.yaml --port 8080
```

For Docker Compose, see [Deploy with Docker](/guide/deploy-docker).

## Go install (from source)

```sh
go install github.com/luowensheng/wave/orchestrator@latest

# The binary lands at $GOBIN/orchestrator (or $HOME/go/bin/orchestrator).
# Symlink as `wave` for ergonomic CLI calls:
ln -s "$HOME/go/bin/orchestrator" /usr/local/bin/wave
```

## Build from source

```sh
git clone https://github.com/luowensheng/wave.git
cd wave
go build -o wave ./orchestrator
./wave version
```

## Verify install

```sh
wave version
# wave v0.1.0 (build: ..., commit: ...)
```

## Next: [Quickstart →](/guide/quickstart)
