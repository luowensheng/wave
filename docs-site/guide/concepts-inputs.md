# Inputs

`inputs:` declares every value a route accepts. Wave parses,
coerces, and validates them before the handler runs. Declared inputs
are the *only* values templates can reference — anything not declared
returns a 500 ("undeclared input") at runtime.

This is the **strict-scope** guarantee: SQL templates can't pull in
arbitrary request values by accident.

## Anatomy

```yaml
inputs:
  - name: user_id          # template key — `{{user_id}}` in SQL
    source: path           # where to read from
    type: int              # coercion target
    required: true
    min: 1
    max: 999999
    pattern: "^[a-z]+"     # regexp (strings only)
    enum: [a, b, c]        # allowed values (strings only)
    default: 0             # used when missing and not required
```

## Sources

| `source` | Reads from |
|---|---|
| `path` | URL path variable `{name}` |
| `query` | `?name=value` |
| `body` | named field of JSON / form / multipart body |
| `form` | alias for `body` with form content-type |
| `header` | request header |
| `cookie` | cookie value |
| `body_raw` | the entire raw body as `[]byte` (use with `type: bytes`) |

## Types

| `type` | Result |
|---|---|
| `string` | `string` (default) |
| `int` | `int64` |
| `float` | `float64` |
| `bool` | `bool` — `true/1/yes` → true |
| `email` | `string` validated as an email |
| `uuid` | `string` validated as a UUID |
| `file` | `*inputs.File` — multipart upload |
| `bytes` | `[]byte` — raw body |
| `array` | `[]any` — JSON array |
| `object` | `map[string]any` — JSON object |

## Validators

| Field | For | Behavior |
|---|---|---|
| `required` | all | 400 if absent |
| `min` / `max` | numeric | range check |
| `min` / `max` | string | length check |
| `pattern` | string | regexp must match |
| `enum` | string | value must be in list |
| `default` | all | used when absent and not required |

A request that fails any input check returns a single 400 with
**every** problem listed (not one-at-a-time, which would force the
client to make N round-trips).

## Strict-scope SQL templating

A `{{name}}` in a `storage-access` `execute:` string is a
function call that emits `?` and appends the value to the params
slice. The function name must match a declared input name —
otherwise the template render fails at request time.

```yaml
inputs:
  - { name: user_id, source: path, type: int, required: true }
storage-access:
  execute: "SELECT * FROM users WHERE id = {{user_id}}"
  #                                       ^^^^^^^^^^^
  #                              looked up in declared inputs;
  #                              becomes "?" + params=[<user_id>]
```

If `execute:` references `{{age}}` and `age` isn't declared, the
request 500s. **This is by design** — it eliminates the entire class
of "I forgot to validate this query param and now it's in my SQL."

## Sources × types: what works where

- `type: file` requires `source: body` (or `form`) AND
  `expected_content_type: multipart/form-data` on the route.
- `type: bytes` requires `source: body_raw`. Useful for kv-stores,
  blob uploads, or routes that need the raw text-plain body.
- `type: array` / `type: object` require `source: body` with JSON
  content-type. Pass them to SQL via `{{jsonArray (raw "name")}}`
  for `json_each` queries.

## Cross-source bodies

For non-JSON bodies, declare it explicitly:

```yaml
expected_content_type: multipart/form-data       # for file uploads
expected_content_type: application/x-www-form-urlencoded
expected_content_type: text/plain               # for type:bytes + body_raw
```

JSON is the default when unset.

## Reading inputs from Go (in plugins)

Plugins receive the full request envelope, including the parsed
inputs map. See [Plugins](/guide/concepts-plugins).

## See also

- [Routes](/guide/concepts-routes)
- [Storage](/guide/concepts-storage) — how `{{name}}` becomes `?`
- [File uploads recipe](/cookbook/file-uploads) — the `type: file` source
