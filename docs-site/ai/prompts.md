# Prompt patterns

Patterns that produce reliable Wave configs from LLMs.

## Anatomy of a good Wave prompt

Three ingredients. Include all of them up front:

1. **Goal** — what HTTP surface you want
2. **Constraints** — auth, validation, scaling expectations
3. **Storage / persistence** — what data lives where

Skipping any of these usually produces a generic answer.

## Pattern 1 — One concrete route

> Write a Wave route at `POST /bookmarks` that:
> - Requires auth via my existing `app` config
> - Accepts JSON: `{url: string, tags: array}` — url must be a
>   real URL, tags max 5 strings of max 30 chars each
> - Persists to a `bookmarks` table with id, user_id, url,
>   tags (JSON), created_at
> - Returns the new id

**Why it works**: complete spec — type, constraints, persistence,
response shape. The model doesn't have to guess.

## Pattern 2 — Convert from another framework

> Convert this Express route to Wave. The Wave config already has
> `storage: app` (sqlite) and `auth: jwt` defined.
>
> ```js
> app.post('/messages', authRequired, async (req, res) => {
>   const { text } = req.body
>   if (!text || text.length > 1000) return res.status(400).json({error:'invalid'})
>   const r = await db.run('INSERT INTO messages(user_id, text) VALUES (?, ?)',
>                          req.user.id, text)
>   res.json({id: r.lastID})
> })
> ```

**Why it works**: source code makes intent unambiguous. The model
maps `app.post` → `method: POST`, `req.user.id` → `{{getUser}}`,
parameterized `?` → `{{text}}`, the validation → `inputs: min/max`.

## Pattern 3 — Build a feature from a user story

> A user can register with email, get a magic link, and start
> tracking habits. Each habit is name + frequency (daily/weekly).
> They can check off a habit for today; we record the timestamp.
> Build the full server.yaml.

**Why it works**: states the model can decompose. The model
infers: tables (`users`, `habits`, `checkins`), routes (signup,
list habits, create habit, check off), auth wiring, and probably
adds a `/me` route without being asked.

## Pattern 4 — Add a single feature to an existing config

> Add rate limiting to my POST /search route. 100 req/min per IP,
> burst 20. On reject return 429 with `{"error":"slow down"}`.

**Why it works**: scoped, asks for only the diff. The model adds
the `limits:` block and the `limits: [...]` field on the route,
leaves everything else alone.

## Pattern 5 — Debug an error

> My Wave route returns 500 with this log:
>
>     undeclared input "user_id"
>
> Here's the route:
>
> ```yaml
> - path: /me
>   method: GET
>   auth: [app]
>   type: storage-access
>   storage-access:
>     source: app
>     execute: "SELECT id FROM users WHERE id = {{user_id}}"
> ```
>
> What's wrong?

**Why it works**: tells the model exactly what to look at. The
correct answer (use `{{getUser}}`, not `{{user_id}}` — `user_id`
isn't a declared input) is in the Claude skill and `llms.txt`.

## Patterns that don't work as well

### Vague

> Make my API faster.

The model will guess. Be specific about what's slow.

### Asking for opinion before facts

> Should I use storage-access or api for this?

The model can guess, but it's better to describe the use case
("I need to call an upstream HTTP API and transform the JSON")
and let it tell you which.

### Asking for things that don't exist

> Use Wave's built-in GraphQL federation.

Federation isn't built in. The model will hallucinate. If you're
not sure whether a feature exists, ask first ("Does Wave have X?")
or search `llms.txt`.

## Speed-running configs

Combine the patterns:

> Build a server.yaml for a SaaS that:
> - Multi-tenant by subdomain (`{tenant}.example.com`)
> - Magic-link auth, per-tenant data scoping
> - REST CRUD on /items with audit log
> - Rate-limit /signup to 5/min, /items to 100/min per user
> - Stripe webhook receiver that fans out to SSE
> - /admin route gated on role=admin
>
> Storage: sqlite at ./data.db. Use one `app` storage,
> namespace tables by tenant via a tenant_id column.

This produces a working starter in one shot if your model has the
skill installed.

## See also

- [Claude Code skill](/ai/claude-code)
- [Cursor + editors](/ai/editors)
- [llms.txt](/ai/llms-txt)
