# syntax=docker/dockerfile:1
#
# Multi-stage build that ships wave as a small, distroless-style
# container. CGO is enabled because the existing sqlite backend depends
# on it; we use a glibc base for ABI compatibility.
#
# Build:   docker build -t wave:dev .
# Run:     docker run --rm -p 8080:8080 -v $PWD/server.yaml:/app/server.yaml wave:dev serve /app/server.yaml --host 0.0.0.0 --port 8080

FROM golang:1.24 AS build
WORKDIR /src
ENV CGO_ENABLED=1
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN go build -trimpath -ldflags="-s -w -X wave/orchestrator/server.Version=$(git rev-parse --short HEAD 2>/dev/null || echo docker)" \
    -o /out/wave ./orchestrator

FROM gcr.io/distroless/cc-debian12:nonroot
COPY --from=build /out/wave /usr/local/bin/wave
WORKDIR /app
EXPOSE 8080
ENTRYPOINT ["/usr/local/bin/wave"]
CMD ["version"]
