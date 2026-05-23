# llms.txt

Wave ships a [`llms.txt`](https://github.com/luowensheng/wave/blob/main/llms.txt)
file at the repo root, following the [llmstxt.org](https://llmstxt.org/)
spec. It's a curated, LLM-readable index of what's in the project.

## Why it exists

LLMs are getting good at "research a project's docs and then help."
The bottleneck is *which* docs to load. A 24 kB CLAUDE.md is too
much for casual queries; the README is too thin for real work.

`llms.txt` is the middle: ~3 kB pointing the model at the right
deeper docs for the job, plus the four critical rules up front so
the model doesn't make obvious mistakes even without loading more.

## What's in it

- One-line project description
- Pointers to the README, CLAUDE.md, CHANGELOG
- Pointers to the docs site sections (guide, cookbook, reference, ai)
- Pointers to the most-referenced runnable demos
- **The four non-negotiable rules** for writing Wave configs
  (SQL parameterisation, declared inputs, methods-vs-method,
  LIMIT 1)

## How to use it

### With Claude (claude.ai or API)

> Read https://raw.githubusercontent.com/luowensheng/wave/main/llms.txt
> then [your actual ask].

The model fetches the file, gets the index + critical rules, and
can decide which deeper docs to load if your task needs them.

### With Cursor / Continue / Aider

These tools have first-class `@web` or URL ingestion. Point them
at `llms.txt` once per session:

```
@web https://raw.githubusercontent.com/luowensheng/wave/main/llms.txt
```

### With ChatGPT / Perplexity / Gemini

Paste the URL into the prompt. They fetch it (Pro / Plus tiers
required for some).

## When llms.txt isn't enough

For deep work — building a complete server.yaml from scratch,
writing a new route type, debugging a tricky template error —
load the full `llms-full.txt`:

```
https://raw.githubusercontent.com/luowensheng/wave/main/docs-site/llms-full.txt
```

::: warning Pending publish
`llms-full.txt` is a concatenated dump of all docs (~50 kB). It
lands in a follow-up commit; until then load CLAUDE.md directly:
https://raw.githubusercontent.com/luowensheng/wave/main/CLAUDE.md
:::

## Keeping it current

`llms.txt` is hand-curated, not generated. When you add a
substantial feature or a new core demo:

1. Edit `llms.txt`
2. Add the section under its category
3. Update the critical rules if any changed
4. Commit

The file is small enough that drift is easy to catch in review.

## See also

- [llmstxt.org](https://llmstxt.org/) — the spec
- [Claude Code skill](/ai/claude-code) — for in-Claude-Code use
- [Prompt patterns](/ai/prompts)
