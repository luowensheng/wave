# Claude Code skill

Wave ships with a built-in Claude Code skill so the model produces
working Wave configs reliably, with no extra prompting from you.

## Installation

The skill lives in the repo at `.claude/skills/wave.md`. To use it
in your own project that uses Wave:

```sh
mkdir -p .claude/skills
curl -sSfL \
  https://raw.githubusercontent.com/luowensheng/wave/main/.claude/skills/wave.md \
  > .claude/skills/wave.md
```

That's it. Claude Code auto-discovers skills in `.claude/skills/`.

## What it does

When you ask Claude to write or modify a Wave config — or it sees
you working in a directory with `server.yaml` — the skill activates
and:

1. **Loads the four non-negotiable rules** about SQL parameterisation,
   declared inputs, `methods:` vs `method:`, and `LIMIT 1`. These
   prevent the most common silent-failure footguns.

2. **Provides the route-type quick reference**: which `type:` to
   use for which job.

3. **Documents the 5-step add-a-route-type checklist** in case
   you're extending Wave itself.

4. **Lists the common YAML idioms** (conditional SQL clauses, JSON
   arrays into IN(…), multi-statement UPDATE+SELECT, predicate
   routing).

5. **Flags things that look right but aren't** — `method: post` +
   `cors_origins:` doesn't work, `{{.Data}}` in SQL leaks output-
   template syntax into SQL, etc.

## What it doesn't do

- Execute code — it's a doc skill, not a tool. Claude still runs
  your usual tools.
- Replace `CLAUDE.md` — that's the canonical reference. The skill
  is a curated summary so Claude doesn't load 24kB of context for
  every "write me a Wave route" request.

## When to update the skill

When the rules in `CLAUDE.md` change — typically on new feature
releases. The skill file should always be a strict subset of what
`CLAUDE.md` says.

To pull the latest:

```sh
curl -sSfL \
  https://raw.githubusercontent.com/luowensheng/wave/main/.claude/skills/wave.md \
  > .claude/skills/wave.md
```

Diff it against your local copy before overwriting if you've
customized it.

## Prompt patterns that work

With the skill installed, you can use short, direct prompts:

> Build me a Wave server that accepts POST /events with a Stripe
> webhook signature, persists each event to SQLite, and broadcasts
> them over SSE.

> Add a route at /admin/users that requires the admin role and
> returns paginated users with a search filter.

> Convert this Express route to Wave: [paste Express handler]

The skill primes Claude with the right vocabulary (`type:`,
`inputs:`, `storage-access`, `match`, etc.) so you don't have to
spell out every constraint.

## Patterns that don't work as well

- "Write me a Wave plugin in Rust" — plugins are out-of-process
  binaries with a JSON contract; Claude needs more concrete
  context (target language, transport choice). Be specific.
- "Optimize this Wave config" — vague. Better: "this route is hitting
  the rate limit too often — reduce burst to 5 per minute and
  bucket per user".

## See also

- [Editors (Cursor + Copilot)](/ai/editors) — schema-based completion
- [Prompt patterns](/ai/prompts) — example prompts and what they produce
- [llms.txt](/ai/llms-txt) — the LLM index file at the repo root
