# Privacy

## Wave never phones home

The `wave` binary makes **zero outbound network requests** by
itself. No telemetry. No analytics. No remote config fetches.
No license check. No "feature toggles" call-home.

When you run `wave serve server.yaml`, the only network traffic
originates from:

- Routes you configure (e.g. `type: forward` to your own backend)
- Plugins you declare (your code in `plugins:`)
- Storage backends you point at (your DB)
- OAuth providers you set up in `auth:`
- Observability exporters you opt into (`otel:` endpoint, etc.)

That's it.

## Verify it

```sh
# Trace every syscall and network call. Should be empty for outbound
# beyond what your config sets up.
strace -e network wave serve /etc/wave/server.yaml --port 8080 2>&1 \
  | grep -E '(connect|sendto)' | head
```

The binary is open source and reproducibly built — you can audit
this claim end-to-end.

## What's logged by default

| Sink | What |
|---|---|
| stdout (JSON) | Request log: method, path, status, duration, request ID, client IP, optional user ID |
| `/metrics` | Per-route counters and histograms (no PII) |
| Audit log | Only if you add `audit:` to a route or use the [audit recipe](/cookbook/audit-log) |
| OTLP traces | Only if `otel:` is configured |

No request body is logged unless you explicitly opt in via a
plugin. No response body is logged.

## Cookies set by Wave

Only the auth cookies you configure under `auth:` (e.g.
`session`). Wave does not set any session, tracking, or analytics
cookies on its own.

## If telemetry is ever added

We commit to:

1. **Opt-in** — never on by default.
2. **Transparent** — documented in detail at install time.
3. **Local-first** — aggregated counts only, never request bodies,
   user IDs, or hostnames.
4. **Single CLI flag to disable** — `wave --no-telemetry serve …`.
5. **Source code visible** — the exporter would be a regular
   plugin, inspectable in this repo.

We have no current plans to add it. This page exists so the
commitment is documented in advance.

## Reporting privacy concerns

Open a [GitHub Security Advisory](https://github.com/luowensheng/wave/security/advisories/new).
Privacy concerns are treated as security issues — same SLA, same
confidentiality.

## See also

- [SECURITY.md](https://github.com/luowensheng/wave/blob/main/SECURITY.md)
- [Code of Conduct](https://github.com/luowensheng/wave/blob/main/CODE_OF_CONDUCT.md)
