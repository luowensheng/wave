# Install

## Pre-built binaries

```sh
# macOS / Linux — auto-detects arch and OS, installs to /usr/local/bin
curl -sSfL https://luowensheng.github.io/wave/install.sh | sh

# Pin a specific version
curl -sSfL https://luowensheng.github.io/wave/install.sh | sh -s -- v0.1.0

# Or, via env var
WAVE_VERSION=v0.1.0 curl -sSfL https://luowensheng.github.io/wave/install.sh | sh

# Or, install to a non-default dir
INSTALL_DIR=$HOME/.local/bin \
  curl -sSfL https://luowensheng.github.io/wave/install.sh | sh

# Or, download manually
curl -sSfL https://github.com/luowensheng/wave/releases/download/v0.1.0/wave_0.1.0_macOS_arm64.tar.gz \
  | tar -xz -C /usr/local/bin
```

::: info nosqlite caveat
Released binaries are built with `-tags=nosqlite` for cross-platform
simplicity. If you use the built-in SQLite storage backend, install
via Docker (which IS sqlite-capable) or `go install` from source.
:::

## Homebrew

::: warning Coming soon
The Homebrew tap (`luowensheng/homebrew-wave`) lands shortly after
v0.1.0. Until then, use the `curl ... | sh` installer or `go install`.
:::

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
