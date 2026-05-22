# Reference

The reference docs are split across two places while the framework
is pre-1.0:

## Canonical reference

[**CLAUDE.md**](https://github.com/luowensheng/wave/blob/main/CLAUDE.md)
in the repo root is the canonical YAML reference. It covers every
route type, every input source, every SQL helper, plus the "Do's
and Don'ts" rules. Editors who clone the repo see it first; LLMs
load it via [`llms.txt`](https://github.com/luowensheng/wave/blob/main/llms.txt).

## Machine-readable

[`docs/server.schema.json`](https://github.com/luowensheng/wave/blob/main/docs/server.schema.json)
is the JSON Schema for `server.yaml`. Tell your editor about it:

::: code-group

```jsonc [VS Code (.vscode/settings.json)]
{
  "yaml.schemas": {
    "https://raw.githubusercontent.com/luowensheng/wave/main/docs/server.schema.json": [
      "server.yaml",
      "**/server.yaml"
    ]
  }
}
```

```yaml [server.yaml header]
# yaml-language-server: $schema=https://raw.githubusercontent.com/luowensheng/wave/main/docs/server.schema.json

default:
  port: 8080
# ...
```

:::

With the schema attached, your editor flags unknown keys, missing
required fields, and type mismatches as you type.

## In-progress

A fully web-rendered reference (route type by route type, sortable,
searchable) is in flight for v0.2. Until then, CLAUDE.md is the
source of truth and the [Cookbook](/cookbook/) covers the common
recipes.
