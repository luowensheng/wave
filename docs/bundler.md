# Frontend Bundler

The bundler is a pure-Go build module that runs at easyserver startup. It concatenates JavaScript files, inlines HTML templates, copies local vendor dependencies, and generates a cache-busted `index.html` — with zero Node, npm, or external tooling required.

## Table of Contents

- [Overview](#overview)
- [How It Works](#how-it-works)
- [Quick Start](#quick-start)
- [Configuration Reference](#configuration-reference)
- [JS File Bundling](#js-file-bundling)
- [Template Inlining](#template-inlining)
- [Local Dependencies](#local-dependencies)
- [index.html Generation](#indexhtml-generation)
- [Cache Busting](#cache-busting)
- [Watch Mode](#watch-mode)
- [Dev vs Prod Workflow](#dev-vs-prod-workflow)
- [Project Structure Example](#project-structure-example)
- [Output Reference](#output-reference)
- [Troubleshooting](#troubleshooting)

---

## Overview

When you ship a frontend app with easyserver, the default dev setup fetches each JS file and HTML template individually — often 20+ HTTP requests before the app is interactive. The bundler eliminates this by producing a single self-contained bundle at server startup.

**What the bundler does:**

- Concatenates JS files in the exact order you specify (glob-friendly)
- Inlines HTML component templates as `<script type="text/x-template">` tags so Vue (or any template-ref framework) finds them in the DOM without any `fetch()` calls
- Copies local vendor files (Vue, marked, etc.) into `dist/` — no CDN links ever injected
- Applies basic minification: strips comments and collapses whitespace
- Writes a SHA-256 cache-busted bundle (`app.bundle.js?v=a1b2c3ef`)
- Generates `dist/index.html` from your template, or a minimal shell if you don't provide one
- Automatically registers a `static` route serving `dist/` at `/`

**What the bundler does NOT do:**

- Inject CDN URLs
- Download anything from the internet
- Require Node, npm, webpack, or any build toolchain
- Transform or transpile JS (no TypeScript, no JSX)

---

## How It Works

The build runs once, synchronously, at the start of `Server.Start()`, before any routes are registered.

```
Server.Start()
    │
    ├─ bundler.Run(cfg)
    │       │
    │       ├─ 1. Resolve JS globs → ordered file list
    │       ├─ 2. Resolve template globs → <script> blocks
    │       ├─ 3. Copy vendor files → dist/libs/
    │       ├─ 4. Concatenate templates + JS
    │       ├─ 5. Minify (strip comments, collapse whitespace)
    │       ├─ 6. SHA-256 hash → cache-bust version string
    │       ├─ 7. Write dist/<bundle_name>
    │       └─ 8. Write dist/index.html
    │
    ├─ Auto-register static route: GET / → dist/
    └─ Register remaining routes from config...
```

If `enabled: false`, `bundler.Run` returns immediately and the static route is not added — the server behaves as if the bundler section doesn't exist.

If the build fails (missing files, bad regex, unreadable vendor file), `Start()` returns an error and the server does not start.

---

## Quick Start

**1. Add a `build` section to your `server.yaml`:**

```yaml
build:
  enabled: true
  dist_dir: "dist"
  bundle_name: "app.bundle.js"
  cache_bust: true
  js_files:
    - "static/app.js"
  index_template: "static/index.html"
```

**2. Make sure your `static/index.html` has a `</body>` tag** — the bundler injects the bundle script tag just before it.

**3. Start the server.** You'll see:

```
[BUNDLE] Built app.bundle.js (1 JS files, 0 templates, 0 deps) -> dist/app.bundle.js
```

The `dist/` directory is now served at `/`.

---

## Configuration Reference

All fields live under the `build:` key in `server.yaml`.

```yaml
build:
  enabled: true           # Required. Set false to skip the build entirely.
  dist_dir: "dist"        # Output directory. Default: "dist".
  bundle_name: "app.bundle.js"  # Output filename. Required when enabled.
  cache_bust: true        # Append ?v=<sha256-prefix> to bundle URL in index.html.

  js_files:               # Ordered list of JS file patterns to concatenate.
    - "static/infra/http.js"
    - "static/features/*.js"
    - "static/io/components/**/*.js"

  templates:              # HTML templates to inline as <script x-template> blocks.
    - pattern: "static/io/**/*View.html"
      id_regex: "io/components/(.+?)/(.+?)View\\.html"
      id_replacement: "$1-$2-template"

  dependencies:           # Local vendor files to copy into dist/.
    - src: "vendor/vue.global.prod.js"
      dest: "libs/vue.js"

  index_template: "static/index.html"  # Optional. Your HTML shell template.

  watch: false            # Watch source files and rebuild on change. Default: false.
  watch_debounce_ms: 300  # Milliseconds to wait after last edit before rebuilding. Default: 300.
```

### Field Descriptions

| Field | Type | Required | Description |
|---|---|---|---|
| `enabled` | bool | yes | `true` runs the build; `false` skips it entirely |
| `dist_dir` | string | no | Output directory path, relative to the config file. Default: `"dist"` |
| `bundle_name` | string | yes (if enabled) | Filename for the concatenated JS output |
| `cache_bust` | bool | no | Appends `?v=<hash>` to the bundle `<script>` tag in `index.html` |
| `js_files` | []string | yes (if enabled) | Glob patterns for JS files to include, in load order |
| `templates` | []Template | no | HTML template files to inline |
| `dependencies` | []Dependency | no | Local vendor files to copy into `dist/` |
| `index_template` | string | no | Path to an HTML file used as the `index.html` base. If omitted, a minimal shell is generated |
| `watch` | bool | no | `true` starts a background watcher that rebuilds on file changes. Default: `false` |
| `watch_debounce_ms` | int | no | Milliseconds to wait after the last detected change before triggering a rebuild. Default: `300` |

#### Template fields

| Field | Type | Description |
|---|---|---|
| `pattern` | string | Glob pattern for HTML template files |
| `id_regex` | string | Go regexp applied to the file path to extract the template ID |
| `id_replacement` | string | Replacement string for the regexp (e.g. `"$1-$2-template"`) |

#### Dependency fields

| Field | Type | Description |
|---|---|---|
| `src` | string | Source path, relative to the config file |
| `dest` | string | Destination path inside `dist_dir` |

---

## JS File Bundling

Files are concatenated in the exact order they appear in `js_files`. Glob patterns are expanded in place, preserving relative order within each pattern.

### Glob syntax

| Pattern | Matches |
|---|---|
| `static/app.js` | Exact file |
| `static/features/*.js` | All `.js` files directly inside `features/` |
| `static/io/components/**/*.js` | All `.js` files anywhere under `components/`, recursively |

**Load order matters.** List dependencies before the code that uses them:

```yaml
js_files:
  - "static/infra/http.js"        # HTTP utility used by everything
  - "static/domain/domain.js"     # Domain models
  - "static/features/*.js"        # Feature modules (use domain)
  - "static/io/store.js"          # Store (uses features)
  - "static/io/components/**/*.js"  # ViewModels (use store)
  - "static/orchestrator/mount.js"  # App entry point (uses everything)
```

### Deduplication

A file matched by multiple patterns is only included once, at the position of its first match.

### Minification

After concatenation, the bundler applies safe, regex-based minification:

- Strips `/* block comments */`
- Strips `// line comments`
- Collapses runs of spaces and tabs to a single space
- Collapses multiple blank lines to one

This is intentionally conservative — it does not parse the JS AST, so it will not break string literals or template literals that happen to contain comment-like sequences. It also does not rename variables or tree-shake unused exports. For production workloads requiring aggressive minification, pre-minify your source files before they are picked up by the bundler.

---

## Template Inlining

HTML component templates are inlined into the bundle as `<script type="text/x-template" id="...">` tags. Frameworks like Vue look up templates by the `id` attribute from the DOM — no `fetch()` needed.

### How the ID is derived

For each matched file:

1. The path segment after `static/` is extracted (e.g. `io/components/ProjectList/ProjectListView.html`)
2. `id_regex` is applied as a Go regexp
3. The match is replaced with `id_replacement`
4. The result is lowercased

**Example:**

```yaml
templates:
  - pattern: "static/io/**/*View.html"
    id_regex: "io/components/(.+?)/(.+?)View\\.html"
    id_replacement: "$1-$2-template"
```

| File | Extracted path | Generated ID |
|---|---|---|
| `static/io/components/ProjectList/ProjectListView.html` | `io/components/ProjectList/ProjectListView.html` | `projectlist-projectlist-template` |
| `static/io/components/TaskBoard/TaskBoardView.html` | `io/components/TaskBoard/TaskBoardView.html` | `taskboard-taskboard-template` |

Adjust `id_regex` and `id_replacement` to match whatever naming convention your components use.

### Referencing inlined templates in Vue

```javascript
const ProjectList = {
  name: 'ProjectList',
  template: '#project-list-template',  // matches id="project-list-template"
};
```

### Templates are injected first

In the bundle file, all `<script type="text/x-template">` blocks appear before any application JS. This ensures the template nodes exist in the DOM before Vue mounts and resolves component templates.

---

## Local Dependencies

The `dependencies` list copies vendor files from your project into `dist/` and records their paths for `index.html` generation. No URLs are ever injected — if the source file doesn't exist on disk, the build fails.

```yaml
dependencies:
  - src: "vendor/vue.global.prod.js"
    dest: "libs/vue.js"
  - src: "vendor/marked.min.js"
    dest: "libs/marked.js"
  - src: "vendor/purify.min.js"
    dest: "libs/purify.js"
```

This produces:

```
dist/
└── libs/
    ├── vue.js
    ├── marked.js
    └── purify.js
```

And in the generated `index.html`:

```html
<script src="/libs/vue.js"></script>
<script src="/libs/marked.js"></script>
<script src="/libs/purify.js"></script>
```

Dependencies are referenced before the application bundle, so they are available when your JS runs.

---

## index.html Generation

### With `index_template`

If you provide `index_template`, the bundler reads that file and injects a `<script>` tag for the bundle immediately before the closing `</body>` tag.

```yaml
index_template: "static/index.html"
```

Your template:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>My App</title>
  <link rel="stylesheet" href="/style.css">
  <script src="/libs/vue.js"></script>
</head>
<body>
  <div id="app"></div>
</body>
</html>
```

Generated `dist/index.html`:

```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <title>My App</title>
  <link rel="stylesheet" href="/style.css">
  <script src="/libs/vue.js"></script>
</head>
<body>
  <div id="app"></div>
<script src="/app.bundle.js?v=a1b2c3ef"></script>
</body>
</html>
```

When using `index_template`, you control all `<script>` and `<link>` tags in `<head>` yourself. The bundler only adds the bundle reference.

### Without `index_template`

If `index_template` is omitted, the bundler generates a minimal HTML shell. Dependency `<script>` tags (from the `dependencies` list) are included automatically.

```html
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>App</title>
<link rel="stylesheet" href="/style.css">
<script src="/libs/vue.js"></script>
<script src="/libs/marked.js"></script>
</head>
<body>
<div id="app"></div>
<script src="/app.bundle.js?v=a1b2c3ef"></script>
</body>
</html>
```

---

## Cache Busting

When `cache_bust: true`, the bundler computes a SHA-256 hash of the final bundle content and appends the first 8 bytes (16 hex characters) as a query string:

```
/app.bundle.js?v=a1b2c3ef45678901
```

This string is written into `index.html` — the file on disk is always named `app.bundle.js` (no hash in the filename), so the `dist/` directory stays clean across rebuilds.

**Effect:** Browsers that have cached the old `index.html` will still cache the old bundle. Once `index.html` is refreshed (it is not cache-busted), the new `?v=...` query string causes the browser to fetch the new bundle.

For immutable long-term caching, configure your reverse proxy to set `Cache-Control: public, immutable, max-age=31536000` on `dist/` assets and a short TTL on `index.html` itself.

When `cache_bust: false`, the `<script src>` tag references just `/app.bundle.js` with no version suffix.

---

## Watch Mode

When `watch: true`, the bundler starts a background goroutine after the initial build. It polls all source files once per second and triggers a rebuild whenever any file's modification time or size changes.

### Configuration

```yaml
build:
  enabled: true
  watch: true
  watch_debounce_ms: 300  # wait 300ms after last change before rebuilding
  # ... rest of config
```

### How debouncing works

When a change is detected, the watcher does not rebuild immediately. It waits `watch_debounce_ms` milliseconds. If another change arrives during that window, the timer resets. The rebuild only fires once the files have been stable for the full debounce period.

This prevents thrashing when an editor saves multiple files in quick succession or writes a file in multiple flushes.

```
edit file A ──┐
edit file B ──┤──── debounce timer resets
edit file A ──┘
                    [300ms of quiet]
                         │
                         └──► rebuild fires once
```

### Tuning `watch_debounce_ms`

| Scenario | Recommended value |
|---|---|
| Single-file edits, fast feedback | `100`–`200` |
| Default — most editors, save-on-focus-loss | `300` |
| Large glob trees, many files saved at once | `500`–`1000` |
| Slow disk / network filesystem | `1000`+ |

### What files are watched

The watcher re-resolves all globs on every poll tick, so **new files are detected automatically** — you don't need to restart the server when you add a component.

Files watched:
- Every file matched by `js_files` globs
- Every file matched by `templates[*].pattern` globs
- The `index_template` file (if set)
- Every `dependencies[*].src` file

### Log output

```
[BUNDLE] File change detected, rebuilding...
[BUNDLE] Built app.bundle.js (12 JS files, 8 templates, 3 deps) -> dist/app.bundle.js
```

If the rebuild fails (e.g. a JS file has a read error), the error is logged and the previous `dist/` output is left in place:

```
[BUNDLE] File change detected, rebuilding...
[BUNDLE] Rebuild failed: read js static/features/foo.js: open static/features/foo.js: no such file or directory
```

### Lifecycle

The watcher goroutine is tied to the `context.Context` passed to `Server.Start()`. It stops cleanly when the server shuts down — no leaked goroutines.

### Watch mode is for development

In production, set `watch: false`. The initial build at startup is sufficient, and polling adds unnecessary overhead.

---

## Dev vs Prod Workflow

### Development (no build)

```yaml
build:
  enabled: false
```

- The bundler is skipped entirely
- The server serves your `static/` source files directly via a static route you define
- Your JS fetches HTML templates at runtime (existing fetch logic)
- Edit → refresh, no build step

### Development (with watch)

```yaml
build:
  enabled: true
  watch: true
  watch_debounce_ms: 300
  # ... rest of config
```

- Initial build runs on startup
- Background watcher rebuilds `dist/` automatically on every save
- Pair with a browser auto-refresh tool or manually reload after the `[BUNDLE]` log line appears

### Production

```yaml
build:
  enabled: true
  watch: false
  dist_dir: "dist"
  bundle_name: "app.bundle.js"
  cache_bust: true
  # ...
```

- The bundler runs once at server startup
- `dist/` is served at `/` via an auto-registered static route
- All templates are inlined — zero template fetches
- Single JS file with cache-bust hash

### Adapting your mount script

Your orchestration JS can detect the mode at runtime:

```javascript
const TEMPLATES_INLINED = document.querySelector('script[type="text/x-template"]') !== null;

if (!TEMPLATES_INLINED) {
  // Dev: fetch templates from static/
  await injectTemplates(COMPONENT_TEMPLATES);
}

// Continue with app.mount() either way
```

This lets the same codebase work in both modes without any conditional build flags.

---

## Project Structure Example

```
myapp/
├── server.yaml
├── static/
│   ├── index.html           ← index_template source
│   ├── style.css
│   ├── infra/
│   │   └── http.js
│   ├── domain/
│   │   └── domain.js
│   ├── features/
│   │   ├── ProjectManagement.js
│   │   └── TaskManagement.js
│   └── io/
│       ├── store.js
│       └── components/
│           ├── ProjectList/
│           │   ├── ProjectListViewModel.js
│           │   └── ProjectListView.html    ← inlined as template
│           └── TaskBoard/
│               ├── TaskBoardViewModel.js
│               └── TaskBoardView.html      ← inlined as template
├── vendor/                  ← local copies of third-party libs
│   ├── vue.global.prod.js
│   └── marked.min.js
└── dist/                    ← generated at startup (do not commit)
    ├── index.html
    ├── app.bundle.js
    └── libs/
        ├── vue.js
        └── marked.js
```

Add `dist/` to `.gitignore` — it is regenerated every time the server starts.

---

## Output Reference

### Startup log line

```
[BUNDLE] Built app.bundle.js (12 JS files, 8 templates, 3 deps) -> dist/app.bundle.js
```

### Bundle file structure

```javascript
// === INLINED TEMPLATES ===
<script type="text/x-template" id="projectlist-projectlist-template">...</script>
<script type="text/x-template" id="taskboard-taskboard-template">...</script>

// === APPLICATION CODE ===
// ... all JS files concatenated and minified
```

### Error messages

| Message | Cause |
|---|---|
| `frontend build failed: no js files matched patterns` | None of the `js_files` globs matched any files |
| `frontend build failed: copy dependency vendor/vue.js: ...` | A `dependencies.src` file does not exist |
| `frontend build failed: invalid id_regex '...': ...` | `id_regex` is not a valid Go regular expression |
| `frontend build failed: read index template: ...` | `index_template` path does not exist |
| `frontend build failed: read js static/foo.js: ...` | A file matched by a glob was deleted between glob resolution and reading |

All errors abort startup — the server will not start with a broken build.

---

## Troubleshooting

**The server starts but I see a blank page.**
Check that `index_template` points to a file with a `</body>` tag. If it is missing, the bundle `<script>` tag will not be injected.

**My component template is not found by Vue.**
Print `document.querySelectorAll('script[type="text/x-template"]')` in the browser console and verify the `id` attribute matches what your component's `template: '#...'` property references. Adjust `id_regex` / `id_replacement` until the generated ID matches.

**Changes to JS files are not picked up.**
The bundler runs once at startup. Restart the server to rebuild. For a live-reload development loop, use `easyserver serve-live` with `enabled: false` and serve source files directly.

**A glob pattern matches files in the wrong order.**
`**` glob expansion uses filesystem walk order, which is alphabetical within each directory. Rename files or switch to explicit exact paths to control order precisely.

**I get `no js files matched patterns` but the files exist.**
Paths in `js_files` are relative to the directory containing `server.yaml` (the server changes its working directory to that location on startup). Double-check the path prefix matches your layout.

**Watch mode rebuilds but the browser still shows the old version.**
The bundle file on disk is updated, but the browser may have cached the old `dist/index.html` or bundle. Hard-refresh (`Cmd+Shift+R` / `Ctrl+Shift+R`) or disable browser cache in DevTools while developing. With `cache_bust: true` the `?v=...` query string in `index.html` changes on every rebuild, so a normal refresh is enough once `index.html` itself is re-fetched.

**Watch mode is not detecting changes.**
The watcher polls once per second using `os.Stat` (mtime + size). Some editors write files atomically by replacing the inode rather than modifying in-place, which can briefly make the old mtime visible. This resolves on the next poll tick (within 1 second). If your filesystem timestamps have coarse resolution (FAT32, some network mounts), increase `watch_debounce_ms` and verify that `stat` shows an updated mtime after saving.
