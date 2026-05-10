// Package scaffold implements `wave init <template> <dir>` — drops
// a working starter project on disk so users can iterate from a known
// good state instead of an empty file.
//
// Templates are kept as a map from relpath → contents. Adding a template
// is a one-line addition to Templates() — no file embeds, no asset
// pipeline, keeps the binary slim.
package scaffold

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// Template is one named starter project.
type Template struct {
	Name        string
	Description string
	Files       map[string]string
}

// All returns every available template, sorted by name.
func All() []Template {
	tpls := []Template{
		apiTemplate(),
		spaTemplate(),
		internalToolTemplate(),
		pluginStarterTemplate(),
		streamingTemplate(),
		oidcAPITemplate(),
		graphQLTemplate(),
	}
	sort.Slice(tpls, func(i, j int) bool { return tpls[i].Name < tpls[j].Name })
	return tpls
}

func oidcAPITemplate() Template {
	return Template{
		Name:        "oidc-api",
		Description: "API gateway protected by an OIDC IdP (Okta / Auth0 / Google / Entra) with role-based access on routes.",
		Files: map[string]string{
			"server.yaml": `default:
  host: localhost
  port: 8080

env:
  OIDC_ISSUER:    { description: "OIDC issuer URL (e.g. https://dev-1234.okta.com/oauth2/default)" }
  OIDC_CLIENT_ID: { description: "OAuth client ID" }

auth:
  corp:
    type: oidc
    issuer: "${ENV:OIDC_ISSUER}"
    client_id: "${ENV:OIDC_CLIENT_ID}"
    token_location: header
    header_scheme: Bearer

routes:
  - path: /api/me
    method: GET
    type: api
    auth: ["corp"]
    api:
      request: { method: GET, url: "https://httpbin.org/get" }

  - path: /api/admin
    method: GET
    type: api
    auth: ["corp"]
    require_roles: ["admin"]            # claims-based RBAC
    cache: { ttl: 30s, key_by_auth: true }
    circuit: { failure_threshold: 5, cooldown: 30s }
    api:
      request: { method: GET, url: "https://httpbin.org/get" }
`,
			"README.md": `# oidc-api template

OIDC-protected API with claims-based RBAC, per-route response cache,
and a circuit breaker against the upstream.

    OIDC_ISSUER=https://accounts.google.com OIDC_CLIENT_ID=... wave serve server.yaml
    curl -H "Authorization: Bearer <id_token>" http://localhost:8080/api/me

Endpoints:
- /api/me        — any authenticated user
- /api/admin     — must have ` + "`admin`" + ` role in the OIDC ` + "`roles`/`groups`" + ` claim
- /healthz /readyz /metrics /admin/ /docs /openapi.json
`,
		},
	}
}

func graphQLTemplate() Template {
	return Template{
		Name:        "graphql",
		Description: "GraphQL endpoint backed by a subprocess plugin resolver (echo placeholder).",
		Files: map[string]string{
			"server.yaml": `default:
  host: localhost
  port: 8080

plugins:
  resolver:
    transport: process
    command: "./resolver"
    timeout: 10s

routes:
  - path: /graphql
    method: POST
    type: graphql
    graphql:
      plugin: resolver
`,
			"resolver/main.go": `package main

import (
	"encoding/json"
	"os"
)

type req struct {
	TriggerKey string          ` + "`json:\"trigger_key\"`" + `
	Body       json.RawMessage ` + "`json:\"body\"`" + `
}
type resp struct {
	Status int         ` + "`json:\"status\"`" + `
	Body   any         ` + "`json:\"body\"`" + `
}

// Toy resolver. Replace with whatever GraphQL library you like.
func main() {
	var r req
	_ = json.NewDecoder(os.Stdin).Decode(&r)
	json.NewEncoder(os.Stdout).Encode(resp{
		Status: 200,
		Body: map[string]any{
			"data": map[string]any{
				"echo": map[string]any{
					"trigger": r.TriggerKey,
					"raw":     json.RawMessage(r.Body),
				},
			},
		},
	})
}
`,
			"README.md": `# graphql template

    go build -o resolver ./resolver
    wave serve server.yaml
    curl -X POST localhost:8080/graphql \
      -H 'Content-Type: application/json' \
      -d '{"query":"{ echo }","operationName":"Demo"}'
`,
		},
	}
}

// Get returns the template with the given name, or false.
func Get(name string) (Template, bool) {
	for _, t := range All() {
		if t.Name == name {
			return t, true
		}
	}
	return Template{}, false
}

// Render writes every file in t under root. Refuses to overwrite an
// existing non-empty directory unless force is true.
func Render(t Template, root string, force bool) error {
	if !force {
		entries, err := os.ReadDir(root)
		if err == nil && len(entries) > 0 {
			return fmt.Errorf("%s is not empty (use --force to overwrite)", root)
		}
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return err
	}
	for rel, body := range t.Files {
		dest := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(dest, []byte(body), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// ── templates ──────────────────────────────────────────────────────────────

func apiTemplate() Template {
	return Template{
		Name:        "api",
		Description: "JSON API gateway with one upstream forward route and an authenticated /me endpoint.",
		Files: map[string]string{
			"server.yaml": `default:
  host: localhost
  port: 8080

env:
  JWT_SECRET:
    description: HMAC secret for JWT signing
    default: change-me-in-prod

auth:
  user_jwt:
    type: jwt
    token_location: header
    header_name: Authorization
    header_scheme: Bearer
    secret: "${ENV:JWT_SECRET}"
    token_duration_seconds: 3600

routes:
  - path: /healthz
    method: GET
    type: forward
    forward:
      forward_url: http://localhost:8080/healthz   # served by the framework

  - path: /upstream/
    method: GET
    type: forward
    forward:
      forward_url: https://httpbin.org

  - path: /me
    method: GET
    type: api
    auth: ["user_jwt"]
    api:
      request:
        method: GET
        url: https://httpbin.org/json
`,
			"README.md": `# api template

This starter exposes:

- ` + "`/upstream/*`" + ` — forwarded to httpbin.org
- ` + "`/me`" + ` — authenticated example route
- Built-ins: ` + "`/healthz` `/readyz` `/metrics` `/admin`" + `

Run:

    JWT_SECRET=local-dev wave serve server.yaml
`,
		},
	}
}

func spaTemplate() Template {
	return Template{
		Name:        "spa",
		Description: "Single-page app with a JS bundle, server-side login, and an API forward.",
		Files: map[string]string{
			"server.yaml": `default:
  host: localhost
  port: 8080

build:
  src_dir: src
  dist_dir: dist
  js_files: ["app.js"]
  watch: true

auth:
  session:
    type: jwt
    token_location: cookie
    cookie_name: session
    secret: "${ENV:SESSION_SECRET:dev-secret-change-me}"
    token_duration_seconds: 86400
    default_logins:
      - { username: admin, password: admin }

routes:
  - path: /api/login
    method: POST
    type: auth-login
    auth-login:
      auth_config: session

  - path: /api/logout
    method: POST
    type: auth-logout
    auth-logout:
      auth_config: session

  - path: /api/upstream/
    method: GET
    type: forward
    auth: ["session"]
    forward:
      forward_url: https://httpbin.org
`,
			"src/app.js": `console.log("hello from wave")
`,
			"src/index.html": `<!doctype html>
<title>spa starter</title>
<h1>spa starter</h1>
<script src="/app.bundle.js"></script>
`,
			"README.md": `# spa template

Hot-reload bundler is on. Edit ` + "`src/app.js`" + ` and refresh.

    wave serve-live server.yaml
`,
		},
	}
}

func internalToolTemplate() Template {
	return Template{
		Name:        "internal-tool",
		Description: "Internal admin tool: SQLite-backed CRUD with auth + IP allowlist.",
		Files: map[string]string{
			"server.yaml": `default:
  host: 0.0.0.0
  port: 8080

ip_filter:
  ip_whitelist: ["10.0.0.0/8", "127.0.0.1/32"]

storage:
  main:
    type: sqlite
    path: ./data.db

auth:
  staff:
    type: jwt
    token_location: cookie
    cookie_name: staff_session
    secret: "${ENV:STAFF_SECRET}"
    token_duration_seconds: 28800
    default_logins:
      - { username: admin, password: admin }

routes:
  - path: /
    method: GET
    type: file
    file:
      file_path: ./index.html

  - path: /api/items
    method: GET
    type: storage-access
    auth: ["staff"]
    storage-access:
      storage: main
      query: "SELECT id, name FROM items ORDER BY id DESC LIMIT 200"
`,
			"index.html": `<!doctype html>
<title>internal tool</title>
<h1>internal tool</h1>
<p>see /api/items (login required)</p>
`,
			"README.md": `# internal-tool template

Locked to RFC1918 by default. Set ` + "`STAFF_SECRET`" + ` and add real users.
`,
		},
	}
}

func pluginStarterTemplate() Template {
	return Template{
		Name:        "plugin-starter",
		Description: "Plugin route + a Go subprocess plugin that echoes its input. Build the plugin then start the server.",
		Files: map[string]string{
			"server.yaml": `default:
  host: localhost
  port: 8080

plugins:
  echo:
    transport: process
    command: "./echo"
    timeout: 5s

routes:
  - path: /echo
    method: POST
    type: plugin
    plugin:
      name: echo
      trigger_key: hello
      response_output:
        echoed: response.echoed
        trigger: response.trigger_key
`,
			"echo/main.go": `package main

import (
	"encoding/json"
	"os"
)

type req struct {
	TriggerKey string          ` + "`json:\"trigger_key\"`" + `
	Body       json.RawMessage ` + "`json:\"body\"`" + `
}
type resp struct {
	Status int            ` + "`json:\"status\"`" + `
	Body   map[string]any ` + "`json:\"body\"`" + `
}

func main() {
	var r req
	_ = json.NewDecoder(os.Stdin).Decode(&r)
	json.NewEncoder(os.Stdout).Encode(resp{
		Status: 200,
		Body:   map[string]any{"echoed": json.RawMessage(r.Body), "trigger_key": r.TriggerKey},
	})
}
`,
			"README.md": `# plugin-starter

    go build -o echo ./echo
    wave serve server.yaml
    curl -X POST localhost:8080/echo -d '{"hi":1}'
`,
		},
	}
}

func streamingTemplate() Template {
	return Template{
		Name:        "streaming",
		Description: "Webhook→SSE pipeline. POST to /webhooks/test, watch /events/payments.",
		Files: map[string]string{
			"server.yaml": `default:
  host: localhost
  port: 8080

connections:
  payments:
    type: auto    # SSE for EventSource, WS for WebSocket
    subscribe_path: /events/payments
    buffer_size: 64
    keep_alive_interval: 15s
    subscribe_cors_origins: ["*"]

routes:
  - path: /webhooks/test
    method: POST
    type: stream-publish
    stream-publish:
      connection: payments
      route_id: payment_events
      event_type: payment
      output:
        payment_id: response.id
        amount:     response.amount
      static_meta:
        source: test
`,
			"README.md": `# streaming

Open one terminal:

    curl -N localhost:8080/events/payments

In another:

    curl -X POST localhost:8080/webhooks/test \
      -d '{"id":"pi_1","amount":2000,"secret":"sk_x"}'

The subscriber receives only ` + "`payment_id`, `amount`, `source`" + ` —
` + "`secret`" + ` is dropped by the field filter.

Discovery: ` + "`GET /api/streams.json`" + ` lists the route_id → endpoint mapping.
`,
		},
	}
}
