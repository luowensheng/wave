# Cursor, Copilot, and other editor AI

Wave ships a JSON Schema for `server.yaml`. Tell your editor about
it once; from then on you get auto-complete, hover-tooltips, and
real-time validation as you type.

## The schema

Public URL (always points at the latest main branch):

```
https://raw.githubusercontent.com/luowensheng/wave/main/docs/server.schema.json
```

For reproducibility, pin to a specific release:

```
https://raw.githubusercontent.com/luowensheng/wave/v0.1.0/docs/server.schema.json
```

## VS Code (also Cursor, Windsurf, any VS Code fork)

Add to `.vscode/settings.json` in your project:

```json
{
  "yaml.schemas": {
    "https://raw.githubusercontent.com/luowensheng/wave/main/docs/server.schema.json": [
      "server.yaml",
      "**/server.yaml",
      "wave.yaml",
      "**/wave.yaml"
    ]
  }
}
```

Requires the [YAML extension by Red Hat](https://marketplace.visualstudio.com/items?itemName=redhat.vscode-yaml)
(Cursor includes this by default).

Once installed:
- **Ctrl-Space** triggers completions for any key
- **Hover** shows the description from the schema
- **Underlines** flag unknown keys / wrong types in real time

## Per-file (no editor config required)

Add a single comment at the top of `server.yaml`:

```yaml
# yaml-language-server: $schema=https://raw.githubusercontent.com/luowensheng/wave/main/docs/server.schema.json

default:
  port: 8080
# ...
```

The YAML language server picks this up automatically.

## GitHub Copilot

Copilot reads your editor's context. With the schema attached, its
suggestions become noticeably better — it stops inventing keys that
don't exist.

For longer prompts, paste the relevant part of CLAUDE.md or
[llms.txt](/ai/llms-txt) into a comment block above the route
you're authoring. Copilot uses recent context heavily.

## Cursor-specific

Cursor's chat (`Cmd/Ctrl + L`) can ingest the docs:

> @web https://luowensheng.github.io/wave/llms-full.txt then add a
> Wave route to my server.yaml that does X.

The first command pulls the full docs into context; the second is
your actual ask.

## Continue.dev / Aider / Cline / Zed AI

Any editor AI that respects YAML language server hints will work
out of the box once the schema is attached. For the rest, paste
the relevant route type's section from CLAUDE.md as context.

## Common failure modes

- **Editor doesn't pick up the schema** → check the YAML extension
  is installed and the `yaml.schemas` glob actually matches your
  filename.
- **Schema is stale** → upstream the URL (always-main) is updated
  per commit; pinned URLs need a manual bump after a release.
- **AI suggests `{{.name}}` in SQL** → the model has training data
  from generic Go templates. Correct it once and the conversation
  stays on track. The Claude skill prevents this by default.

## See also

- [Claude Code skill](/ai/claude-code)
- [Prompt patterns](/ai/prompts)
- [llms.txt](/ai/llms-txt)
