<!--
Thanks for opening a PR! A few notes:

- Title format: Conventional Commits — feat:, fix:, docs:, refactor:,
  test:, chore:. Examples: `feat(match): support exists operator`.
- Keep PRs focused — one concern per PR.
- For large changes, please open an issue or Discussion first to
  discuss design before spending time on implementation.
-->

## What changed?

<!-- 1-3 sentences explaining the change. -->

## Why?

<!-- Use case, bug, or motivation. Link issues with `Fixes #123`. -->

## How was it tested?

<!--
Concrete steps a reviewer can replicate. "Tests pass" is not enough;
say which tests, or which curl commands, or which demo you ran.
-->

## Pre-flight checklist

- [ ] `go vet ./...` clean
- [ ] `go test -race ./...` passes
- [ ] New code has tests (`*_test.go`)
- [ ] Touched YAML behavior? Updated or added an `examples/apps/*` demo
- [ ] Touched user-facing behavior? Added a `CHANGELOG.md` entry under `[Unreleased]`
- [ ] Touched CLAUDE.md material (code style, route-type conventions)? Updated it
- [ ] PR title follows Conventional Commits

## Notes for reviewer

<!-- Anything reviewers should look at first, known limitations, follow-up work. -->
