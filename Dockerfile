# syntax=docker/dockerfile:1
#
# Multi-stage build that ships wave as a small, distroless container.
# CGO is enabled because the built-in sqlite backend depends on it;
# we use a glibc base for ABI compatibility. Final image is distroless
# (~25 MB) running as a non-root user.
#
# Build:   docker build -t wave:dev .
# Run:     docker run --rm -p 8080:8080 \
#            -v $PWD/server.yaml:/app/server.yaml wave:dev \
#            serve /app/server.yaml --host 0.0.0.0 --port 8080

FROM golang:1.26 AS build
WORKDIR /src

# Build-time metadata injected as ldflags into the binary so
# `wave version` reports the correct identifiers in production.
ARG VERSION=dev
ARG COMMIT=none

ENV CGO_ENABLED=1
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath \
    -ldflags="-s -w -X main.version=${VERSION} -X main.commit=${COMMIT}" \
    -o /out/wave ./orchestrator

# Distroless nonroot — minimal attack surface, no shell, no package
# manager, runs as UID 65532. Has CA certs and tzdata baked in for
# HTTPS calls and timezone-aware scheduling.
FROM gcr.io/distroless/cc-debian12:nonroot
COPY --from=build /out/wave /usr/local/bin/wave
WORKDIR /app
EXPOSE 8080
# No HEALTHCHECK directive — distroless has no shell/curl, and
# orchestrators (Kubernetes, Fly, Nomad) define their own probes
# against the framework-built-in /healthz and /readyz endpoints.
# See docs-site/guide/deploy-* for examples.
ENTRYPOINT ["/usr/local/bin/wave"]
CMD ["help"]
