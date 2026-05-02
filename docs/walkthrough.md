# easyserver — Developer Walkthrough

easyserver is a configuration-driven HTTP server. You describe your server in a
YAML file (`commands.yaml`) and run it with one command. It handles static
files, file serving, API proxying, HTML templates, authentication, SQLite/
filesystem storage, process execution, and more — with no code required for the
common cases.

---

## Quick Start

```bash
go run ./orchestrator serve commands.yaml
```

Or with a host/port override:

```bash
go run ./orchestrator serve commands.yaml --host 0.0.0.0 --port 8080
```

---

## Project Layout (VHCO Architecture)

```
easyserver/
├── domain/         # Pure types: User, Session, File, StorageConfig
├── features/       # Capability shapes (struct-of-funcs): Authentication, Storage, Bundling…
├── usecases/       # Per-route config DTOs + injected function-type declarations
│   ├── auth_login/
│   ├── forward/
│   ├── static_serve/
│   └── … (14 route families)
├── io/http/        # Thin HTTP adapters — one folder per route family
│   ├── auth_login/handler.go
│   ├── forward/handler.go
│   └── …
├── infra/          # Raw adapters with no system knowledge
│   ├── http/       # Logging middleware, TLS generation, CSRF store
│   ├── jwt/        # HS256 sign / parse
│   ├── cookies/    # Cookie policy builder
│   ├── sessions/   # In-memory session store
│   ├── users/      # In-memory + SQLite user stores
│   ├── sqlite/     # SQLite storage backend
│   ├── filesystem/ # Filesystem storage backend
│   ├── bundler/    # esbuild wrapper
│   ├── render/     # Go template + markdown rendering
│   ├── process/    # subprocess execution
│   └── …
└── orchestrator/   # Entry point + all DI wiring
    ├── main.go
    ├── features/   # make_authentication.go, make_storage.go (factories)
    ├── server/     # Server lifecycle, route dispatch
    └── usecases/   # wire.go — binds injected function vars to features
```

**Reading rule**: to understand what the server *is*, read `domain/`. To
understand what it *can do*, read `features/`. To understand *how* it does it,
read `usecases/` + `io/http/`. To understand *what it talks to*, read `infra/`.
To understand *how it's wired together*, read `orchestrator/`.

---

## commands.yaml Reference

Every project starts with a `commands.yaml` that describes your server. Here is
a complete reference with all supported sections.

```yaml
# Optional: default host/port (overridden by --host/--port flags)
default:
  host: "127.0.0.1"
  port: 8080

# Optional: named CLI args you can pass when starting the server.
# Access them inside route config with {{.args.my_arg}}
args:
  api_url:
    default: "https://api.example.com"
    description: "URL of the upstream API"

# Optional: environment variable declarations (validated at startup)
env:
  DATABASE_URL:
    description: "Connection string for the database"

# Optional: HTTPS configuration
https_config:
  ssl_certfile: "./certs/server.crt"
  ssl_keyfile:  "./certs/server.key"
  generate: true          # auto-generate self-signed cert if file missing
  organization: ["Acme"]
  dns_names: ["localhost", "myapp.local"]
  common_name: "localhost"

# Optional: global IP filter
ip_filter:
  ip_whitelist: ["127.0.0.1", "192.168.1.0/24"]
  ip_blacklist: []

# Optional: asset bundling (esbuild)
build:
  entry_point: "./frontend/main.ts"
  output_dir:  "./static/dist"
  watch: true   # rebuild on file change

# Optional: named authentication configurations
auth:
  admin:
    type: jwt
    token_location: cookie     # "cookie" | "header" | "both"
    cookie_name: auth_token
    token_duration_seconds: 86400
    redirect_on_failure: /login
    redirect_on_success: /dashboard
    user_store: sqlite          # "memory" | "sqlite"
    default_logins:
      - username: admin
        password: $ADMIN_PASSWORD  # reads env var ADMIN_PASSWORD

# Optional: named storage backends
storage:
  db:
    type: sqlite
    path: "./data/app.db"
    tables:
      posts:
        columns: [id, title, body, created_at]
      users:
        columns: [id, email, name]
  uploads:
    type: filesystem
    path: "./uploads"

# Routes
routes:
  - path: /
    method: GET
    type: file
    file:
      filepath: ./public/index.html

  - path: /static/
    method: GET
    type: static
    static:
      dir: ./public

  # … see route examples below
```

---

## Sample Projects

### 1. Static Site

Serve HTML, CSS, JS from a directory.

```yaml
# commands.yaml
default:
  port: 3000

routes:
  - path: /
    method: GET
    type: file
    file:
      filepath: ./dist/index.html
      catch_all: true    # SPA: serve index.html for all unknown paths

  - path: /assets/
    method: GET
    type: static
    static:
      dir: ./dist/assets
```

```bash
go run ./orchestrator serve commands.yaml
# → http://127.0.0.1:3000
```

---

### 2. API Reverse Proxy

Proxy all `/api/` requests to an upstream service.

```yaml
default:
  port: 4000

args:
  upstream:
    default: "http://localhost:9000"
    description: "Upstream API URL"

routes:
  - path: /api/
    type: forward
    forward:
      forward_url: "{{.args.upstream}}/api/"
      include_headers:
        - ["X-Internal", "true"]
      allow_insecure_requests: false
      timeout: "30s"

  - path: /
    method: GET
    type: static
    static:
      dir: ./public
```

```bash
go run ./orchestrator serve commands.yaml --upstream http://localhost:9000
```

---

### 3. Login + Dashboard App (JWT Auth)

Full login/signup/logout with JWT cookies and protected routes.

```yaml
default:
  port: 5000

auth:
  app:
    type: jwt
    token_location: cookie
    cookie_name: session
    token_duration_seconds: 3600        # 1 hour
    redirect_on_failure: /login
    redirect_on_success: /dashboard
    user_store: sqlite
    params:
      db_path: "./data/users.db"
    default_logins:
      - username: admin
        password: $ADMIN_PASSWORD

routes:
  # Public pages
  - path: /
    method: GET
    type: file
    file:
      filepath: ./pages/home.html

  - path: /login
    method: GET
    type: file
    file:
      filepath: ./pages/login.html

  # Auth endpoints
  - path: /auth/login
    method: POST
    type: auth-login
    auth-login:
      for: app
      username_field: username
      password_field: password
      redirect_on_success: /dashboard
      redirect_on_failure: /login

  - path: /auth/signup
    method: POST
    type: auth-signup
    auth-signup:
      for: app
      username_field: username
      password_field: password
      confirm_password_field: confirm_password

  - path: /auth/logout
    method: POST
    type: auth-logout
    auth-logout:
      for: app

  # Protected dashboard (requires auth)
  - path: /dashboard
    method: GET
    type: file
    auth: [app]
    file:
      filepath: ./pages/dashboard.html
      is_template: true   # renders with {{.User.Username}} etc.
```

**Login page** (`./pages/login.html`):
```html
<!DOCTYPE html>
<html>
<body>
  <form method="POST" action="/auth/login">
    <input name="username" placeholder="Username">
    <input name="password" type="password" placeholder="Password">
    <button type="submit">Log in</button>
  </form>
</body>
</html>
```

**Dashboard template** (`./pages/dashboard.html`):
```html
<!DOCTYPE html>
<html>
<body>
  <h1>Welcome, {{call .GetUser | .Username}}!</h1>
  <form method="POST" action="/auth/logout">
    <button type="submit">Log out</button>
  </form>
</body>
</html>
```

```bash
ADMIN_PASSWORD=secret go run ./orchestrator serve commands.yaml
```

---

### 4. SQLite CRUD API

Serve a SQLite-backed REST-style API using the `storage-access` route type.

```yaml
default:
  port: 6000

storage:
  db:
    type: sqlite
    path: "./data/blog.db"
    tables:
      posts:
        columns: [id, title, body, author, created_at]

routes:
  # List all posts
  - path: /posts
    method: GET
    type: storage-access
    storage-access:
      source: db
      execute: "SELECT * FROM posts ORDER BY created_at DESC"
      response_content_type: application/json
      output_template: "{{. | json}}"

  # Create a post (POST body: title, body, author)
  - path: /posts
    method: POST
    type: storage-access
    storage-access:
      source: db
      execute: "INSERT INTO posts (title, body, author, created_at) VALUES ({{.title}}, {{.body}}, {{.author}}, datetime('now'))"
      response_content_type: application/json
      output_template: '{"ok": true}'
```

---

### 5. Run a Script / Process

Execute a shell command and stream its output.

```yaml
default:
  port: 7000

routes:
  - path: /run/build
    method: POST
    type: process
    process:
      execute: "npm run build"
      execute_path: "./frontend"
      return_file: false

  - path: /run/report
    method: GET
    type: process
    process:
      execute: "python3 generate_report.py"
      execute_path: "./scripts"
      return_file: true
      output_template: "{{.output}}"
```

---

### 6. Dynamic Forwarding

Forward requests where the target URL is constructed from query params or path
segments.

```yaml
routes:
  - path: /proxy/
    type: dynamic-forward
    dynamic-forward:
      # The target URL is built from the request at runtime
      allow_insecure_requests: true
```

---

### 7. Serving Built-in Frontend Dependencies

Serve the bundled Vue / Axios helpers that ship with easyserver for building
quick admin UIs.

```yaml
routes:
  - path: /deps/
    type: dependencies
    dependencies: {}
```

The route mounts pre-bundled files (Vue 3, Axios, component library) at
`/deps/`. Link them from your HTML:

```html
<script src="/deps/vue3.js"></script>
<script src="/deps/lib.js"></script>
```

---

### 8. IP Filtering

Block or allow clients by IP range.

```yaml
ip_filter:
  ip_whitelist:
    - "127.0.0.1"
    - "10.0.0.0/8"
  ip_blacklist:
    - "1.2.3.4"

routes:
  - path: /admin/
    method: GET
    type: static
    static:
      dir: ./admin
```

The filter applies to every route in the server. For per-route filtering add
`ip_whitelist` / `ip_blacklist` to the route itself:

```yaml
routes:
  - path: /secret
    method: GET
    type: file
    ip_whitelist: ["192.168.1.100"]
    file:
      filepath: ./secret.html
```

---

### 9. Permanent / Temporary Redirect

```yaml
routes:
  - path: /old-page
    type: redirect
    redirect:
      redirect_url: "https://example.com/new-page"
      status_code: 301    # 301 permanent, 302 temporary (default)
```

---

### 10. File Server (Directory Listing)

Serve a directory with browsable listing.

```yaml
routes:
  - path: /files/
    type: file-server
    file-server:
      dir: ./shared
```

---

## Auth: JSON API vs Browser Flows

easyserver detects whether a request comes from a browser (by inspecting the
`Accept` and `User-Agent` headers) and adjusts its response automatically:

| Scenario | Browser (GET) | API / POST |
|---|---|---|
| Auth fails | 302 redirect to `redirect_on_failure` | 401 JSON `{"success":false}` |
| Login success (cookie) | 302 redirect to `redirect_on_success` | 200 JSON with `{"value":"<token>"}` |
| Login success (header) | 200 JSON | 200 JSON |

This means the same auth endpoint works for both a traditional HTML form and a
JavaScript `fetch()` call.

---

## Route Types Reference

| `type:` value | Config key | Purpose |
|---|---|---|
| `static` | `static` | Serve files from a directory |
| `file` | `file` | Serve a single file (optionally a Go template) |
| `file-server` | `file-server` | Directory listing + file download |
| `forward` | `forward` | Reverse proxy to upstream URL |
| `dynamic-forward` | `dynamic-forward` | Forward with runtime URL construction |
| `redirect` | `redirect` | HTTP redirect (301/302) |
| `content` | `content` | Serve rendered markdown/template content |
| `api` | `api` | JSON path extraction + transform |
| `auth-login` | `auth-login` | Handle login form / JSON login |
| `auth-signup` | `auth-signup` | Handle signup form / JSON signup |
| `auth-logout` | `auth-logout` | Revoke session + clear cookie |
| `storage-access` | `storage-access` | SQLite / filesystem query/command |
| `process` | `process` | Run a subprocess, optionally stream output |
| `dependencies` | `dependencies` | Serve built-in frontend library files |

---

## Route Common Fields

Every route supports these fields regardless of type:

```yaml
routes:
  - path: /protected          # URL path prefix (required)
    method: GET               # HTTP method (default: GET); or use methods: [GET, POST]
    methods: [GET, POST]      # Allow multiple methods
    type: file                # Route type (required)
    auth: [admin]             # List of auth config names — ALL must pass
    validate_csrf: true       # Validate CSRF token for this route
    include_csrf: true        # Generate + include CSRF token in response
    ip_whitelist: []          # Per-route IP allow-list
    ip_blacklist: []          # Per-route IP block-list
    description: "My route"  # Shown in discovery endpoint
```

---

## Discovery Endpoints

Add these to your config to get auto-generated API documentation:

```yaml
json_discovery_route_path: /routes.json
html_discovery_route_path: /routes
```

- `/routes.json` — machine-readable route list (method, path, type, description)
- `/routes` — human-readable HTML table

---

## Live Reload

```bash
go run ./orchestrator serve-live commands.yaml
```

The server watches `commands.yaml` for changes and automatically restarts when
it detects a modification. Useful during development.

---

## HTTPS with Auto-Generated Certificate

```yaml
https_config:
  ssl_certfile: "./certs/server.crt"
  ssl_keyfile:  "./certs/server.key"
  generate: true
  dns_names: ["localhost"]
  common_name: "localhost"
```

If the cert file doesn't exist, easyserver generates a self-signed RSA-2048
certificate valid for 1 year at startup. To use a real certificate, set
`generate: false` and provide the paths.

---

## Template Variables in Config

YAML string values support Go template syntax. Available variables:

```yaml
args:
  prefix:
    default: "/app"

routes:
  - path: "{{.args.prefix}}/dashboard"
    type: file
    file:
      filepath: "./pages/{{.args.prefix}}/dashboard.html"
```

---

## Architecture for Contributors

easyserver follows **VHCO** (Vertical Hierarchy, Closed-world Operations). The
rule is simple: each layer only imports from layers below it, and each layer
declares the interfaces it needs itself (consumer-owned protocols).

```
orchestrator  →  features  →  usecases  →  io/http
     ↓               ↓            ↓
   infra/*        domain/      domain/
```

- **domain/** — pure Go structs, no logic, no imports outside stdlib
- **features/** — capability struct shapes (`Authentication{ValidateRequest, ...}`)
- **usecases/** — config DTOs + function-type declarations (no feature imports)
- **io/http/** — thin HTTP handlers that call injected function types
- **infra/** — concrete adapters (JWT, SQLite, cookies, TLS, …); no system knowledge
- **orchestrator/** — the only layer that imports concrete types across modules
  and writes the closures that satisfy each consumer's protocol

To add a new route type:
1. Create `usecases/<name>/config.go` — YAML config struct, no feature imports
2. Create `io/http/<name>/handler.go` — `NewHandler(config, featureFn) http.HandlerFunc`
3. Add a type alias in `usecases/routes/<name>_config.go`
4. Register the new type in `orchestrator/server/route.go`
5. Wire the feature function in `orchestrator/usecases/wire.go`
