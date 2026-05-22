# Wave + AI agents

Wave is designed to be **agent-writable**: its configuration surface
is YAML (LLMs produce reliable YAML), its conventions are documented
in one place (CLAUDE.md), and we ship an `llms.txt` so models can
discover the rules quickly.

## What ships in the box

- **`llms.txt`** at the repo root — [llmstxt.org](https://llmstxt.org/)-format
  index of the most useful docs for LLMs.
- **`llms-full.txt`** — concatenated docs in one file, optimized for
  loading into an LLM context window.
- **`docs/server.schema.json`** — JSON Schema for `server.yaml`.
  Editors (VS Code, Cursor, IntelliJ) auto-complete YAML when you
  associate this schema with files matching `server.yaml`.
- **Claude Code skill** — a `.claude/skills/wave.md` file that
  primes Claude with Wave conventions and idioms.
- **57 runnable demo apps** — concrete worked examples for almost
  every common pattern. LLMs reference these well.

## How to use Wave with…

- [**Claude Code**](/ai/claude-code) — install the skill, see prompt patterns
- [**Cursor / Copilot / Continue**](/ai/editors) — schema association + workspace pinning
- [**Prompt patterns**](/ai/prompts) — patterns that produce reliable output
- [**llms.txt**](/ai/llms-txt) — what's in it and why

## Why YAML beats code for LLM authoring

LLMs hallucinate code idioms (wrong imports, deprecated APIs, type
mismatches). YAML constrains the surface — a `type: storage-access`
route can only have certain keys. With a JSON Schema attached, an
editor catches violations instantly, and an LLM that's been shown
the schema rarely deviates.

The trade-off: less flexibility on the edges. When you need custom
Go logic, drop into a plugin and stay in code. Everything else is
YAML.

## The non-negotiable rules

These rules are repeated in `llms.txt` and `CLAUDE.md` because
violating them creates silent runtime failures:

1. **SQL parameterisation.** `{{name}}` becomes `?`. NEVER use
   `{{.name}}` (dot-notation) — that's SQL injection.
2. **Declared inputs.** Every `{{name}}` referenced in SQL must
   appear in the route's `inputs:` list.
3. **Method-bound + CORS.** Use `methods: [POST]` (plural), not
   `method: post`, when `cors_origins:` is set.
4. **Single-row hint.** Add `LIMIT 1` to lookup queries so `.Data.col`
   works in templates.

An agent that follows these four rules will produce working Wave
configs ~95% of the time on the first try.
