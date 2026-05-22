# Contributing to Wave

First — thanks for considering a contribution. Wave is in pre-1.0,
which means feedback, issues, and PRs have outsized impact right now.

This guide covers everything you need to land your first change.

---

## Quick start

```bash
# Clone & build
git clone https://github.com/luowensheng/wave.git
cd wave
go build ./...

# Run an example
go run ./orchestrator serve examples/apps/url-shortener/server.yaml --port 8102
curl http://localhost:8102/healthz

# Run the test suite
go test ./...
```

You'll need **Go 1.24+** and a Unix-like shell (Linux / macOS / WSL).
Windows builds work but the day-to-day dev loop assumes a POSIX shell.

---

## Where things live

```
wave/
  CLAUDE.md              ← developer guide (read this if you're adding code)
  orchestrator/          ← server boot, route wiring, scheduler
  infra/                 ← pure infrastructure (storage, plugins, connections, ...)
  usecases/              ← route-type business logic, one package per type
  io/http/               ← request/response shaping helpers
  examples/apps/         ← runnable demo configurations (57 of them)
  docs/                  ← reference documentation
```

For an architectural deep dive, read [CLAUDE.md](CLAUDE.md). It is the
canonical source of truth on conventions; this file just covers the
contribution process.

---

## Filing an issue

Use the issue templates — they're forms, so you can't miss the required
fields:

- **Bug** → reproduction steps + expected vs actual behavior
- **Feature** → use case first, proposed solution second
- **Question** → consider [GitHub Discussions](https://github.com/luowensheng/wave/discussions) instead if it's open-ended

Before filing, search existing issues — duplicates are common on
fast-moving projects.

---

## Sending a pull request

### 1. Branch from `main`

```bash
git checkout -b feat/your-thing
```

Branch naming: `feat/…`, `fix/…`, `docs/…`, `refactor/…`, `test/…`,
`chore/…`. Not strict but appreciated.

### 2. Commit message style

We follow [Conventional Commits](https://www.conventionalcommits.org/).
Examples:

```
feat(match): support exists operator on header predicates
fix(cors): answer OPTIONS without Origin header
docs(quickstart): correct sqlite path in storage example
test(auth_login): cover signup-required flow
```

PR titles should follow the same format — they become the squash-merge
commit message.

### 3. Make the change

**Required for every PR:**

- New code has tests (`*_test.go`). Bug fixes have regression tests.
- `go vet ./...` clean
- `go test ./... -race` passes
- If you touched YAML behavior, an `examples/apps/*/server.yaml` covers it
- New route types follow the 5-step checklist in CLAUDE.md

**For larger changes:** open an issue or Discussion *first* so we can
talk about design before you spend a weekend on it.

### 4. Open the PR

The PR template asks two things:
- A short description of *what* and *why*
- A "I tested this" checklist

Keep PRs focused — one concern per PR. Multiple small PRs land faster
than one big one.

### 5. Code review

We try to respond within 24 hours on weekdays. If you don't hear back
in three days, ping the PR — sometimes things slip.

When a maintainer approves and CI is green, they'll squash-merge.

---

## Adding a new route type

Wave's strongest extension point is route types. The full step-by-step
is in [CLAUDE.md](CLAUDE.md) — "Adding a new route type" section. Short
version:

1. Create `usecases/<name>/config.go` with a `Config` struct and a
   `CreateRoute(method, path, data) (http.HandlerFunc, error)` method.
2. Create `usecases/routes/<name>_config.go` exporting a type alias.
3. Add the `*Config` field to the `Route` struct in
   `orchestrator/server/route.go`.
4. Add the type case in `getRouteConfig()`.
5. Wire any dependencies in `InitDependencies` in `servers.go`.
6. Write a `*_test.go`.

Look at how `usecases/match/` (the newest route type) is structured —
it's a clean reference.

---

## Adding a new plugin

Plugins are out-of-process binaries that speak a small JSON contract.
See [docs/plugins.md](docs/plugins.md) for the full contract.

A plugin template repo lives at
`github.com/luowensheng/wave-plugin-template` — fork it, change the
business logic, ship.

---

## Code style

We follow standard Go idioms. From [CLAUDE.md](CLAUDE.md):

- Standard library first, frameworks second, generics rarely.
- `any` over `interface{}`.
- Lowercase error strings, no trailing punctuation.
- Early returns over deep nesting.
- Exported symbols need doc comments.
- No `init()` functions — wire explicitly.
- No `log.Fatal` in `usecases/` — return errors.
- Struct tags `yaml:"…"` + `json:"…"` on every field with `omitempty`
  unless the zero value is meaningful.

Run `gofmt` (or let your editor handle it) before committing.

---

## Releasing (maintainers only)

Releases are tag-driven. From `main` after merging a release-prep PR:

```bash
git tag v0.1.0
git push origin v0.1.0
```

The release workflow handles the rest: signed binaries, SBOM, Docker
image, Homebrew formula update. See `.github/workflows/release.yml`.

---

## Conduct

By participating you agree to the [Code of Conduct](CODE_OF_CONDUCT.md).
TL;DR: be kind, assume good intent, criticize ideas not people.

---

## License

By contributing you agree your contributions are licensed under
Apache-2.0 (the same license as the project). You retain copyright
on your contributions.
