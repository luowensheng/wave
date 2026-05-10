# Studio

Studio is a local web UI that manages multiple wave projects from
one place: register, supervise, inspect routes, exercise endpoints, and
watch live logs and metrics.

## Quick start

```sh
wave studio
# Studio running at http://127.0.0.1:8081/?t=<token>
```

The first launch generates a 32-byte token at `~/.wave/studio.token`
(mode `0600`). The opening URL embeds that token; the page reads it,
sets a `studio_token` cookie, and strips it from the URL. Every
`/api/*` request requires either the cookie or
`Authorization: Bearer <token>`.

Flags:

- `--host 127.0.0.1` — bind host. Studio binds locally only.
- `--port 8081` — HTTP port.
- `--data-dir ~/.wave` — registry + token state directory.
- `--no-browser` — do not auto-open the URL.

## Project supervision

Each project is one entry in `~/.wave/projects.json`. Studio
spawns supervised wave child processes via `os/exec`:

```
<self-binary> serve <project_path>/<config_file>
```

The child binary defaults to `os.Executable()`, so Studio always
spawns the same binary it was launched as — no `$PATH` confusion.
Stdout / stderr are captured into a 1000-line ring buffer per project
and fanned out to SSE log subscribers.

Crashed processes are auto-restarted up to 3 times in 60 seconds, then
left in the `crashed` state for manual inspection.

## Route tester

The tester reads the project's `server.yaml` to learn its `host:port`
(falling back to `localhost:8080` when unset), then issues an HTTP
request from the studio process and returns the response — status,
headers, body, duration — to the browser. The project must be running.

## Metrics

The Metrics tab proxies `GET /metrics` from the running project and
parses the top-level wave counters
(`wave_http_requests_total`, `wave_plugin_calls_total`)
into a small table. The raw Prometheus text is also shown, refreshed
every 5 seconds.

## Caveats

- Studio binds to `127.0.0.1` only. For remote management use
  `ssh -L 8081:127.0.0.1:8081 host`.
- Unregistering a project does **not** delete files on disk.
- Frontend is vanilla JS / CSS / HTML, embedded into the binary via
  `//go:embed all:web/*` — no build step.
- Test request history is in-memory only and lost on Studio restart.
