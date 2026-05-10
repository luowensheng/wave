# url-shortener

Slug -> URL with a SQLite `links` table. Shows how to pair
`storage-access` with a redirect-style response.

## Run it

```sh
wave serve examples/apps/url-shortener/server.yaml --port 8102
```

## Try it

```sh
curl -s -X POST localhost:8102/shorten \
  -H 'content-type: application/json' \
  -d '{"slug":"github","url":"https://github.com"}'
# → {"short": "/gh"}

# Open in a browser — meta-refresh + JS hop.
open http://localhost:8102/r/github

# JSON lookup
curl -s localhost:8102/links/gh
```

## What to look at

- `server.yaml` keeps slug generation client-side so the SQL stays
  one-line. For server-side hashing, swap `INSERT` for a CTE that
  derives slug from `random()`.
- The redirect route returns text/html with both a `<meta refresh>`
  AND a JS `location.replace` — `storage-access` cannot emit a 302
  directly (it always returns 200 with a body), so this is the
  pragmatic substitute.

## Caveats

- Slugs are user-chosen for demo purposes (3-32 chars, regex-validated).
- A real shortener would also need a uniqueness retry loop on slug
  collisions; this one just lets the INSERT fail.
