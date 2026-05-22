package servers

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
)

// loadFrom is a test helper: writes nothing, just calls loadConfig on
// an existing fixture path and returns the resolved Config.
func loadFrom(t *testing.T, path string) *Config {
	t.Helper()
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatalf("loadConfig(%s): %v", path, err)
	}
	return cfg
}

// ---- path-join edges -------------------------------------------------

func TestJoinPrefixEdges(t *testing.T) {
	cases := []struct{ prefix, p, want string }{
		{"/rss", "/feeds", "/rss/feeds"},
		{"rss/", "feeds", "/rss/feeds"},
		{"/rss", "/", "/rss"},
		{"", "/feeds", "/feeds"},
		{"/", "/feeds", "/feeds"},
		{"/rss", "", "/rss"},
	}
	for _, c := range cases {
		if got := joinPrefix(c.prefix, c.p); got != c.want {
			t.Errorf("joinPrefix(%q,%q) = %q want %q", c.prefix, c.p, got, c.want)
		}
	}
	// nested compose
	p := joinPrefix(joinPrefix("/a", "/b"), "/feeds")
	if p != "/a/b/feeds" {
		t.Errorf("nested = %q want /a/b/feeds", p)
	}
}

// ---- applyPrefix on every locked field, none of the outbound -------

func TestApplyPrefix_LockedInboundFields(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "modules/m.yaml", `routes:
  - path: /login
    method: POST
    type: auth-login
    auth-login:
      redirect_on_success: /home
      redirect_on_failure: /login?err=1
  - path: /signup
    method: POST
    type: auth-signup
    auth-signup:
      redirect_on_success: /welcome
      redirect_on_failure: /signup?err=1
  - path: /logout
    method: POST
    type: auth-logout
    auth-logout:
      redirect_on_success: /bye
      redirect_on_failure: /oops
  - path: /magic/req
    method: POST
    type: magic-link-request
    magic-link-request:
      callback_url: /magic/verify
  - path: /magic/consume
    method: GET
    type: magic-link-consume
    magic-link-consume:
      success_redirect: /dashboard
  - path: /fwd
    method: GET
    type: forward
    forward:
      forward_url: http://upstream.local/keepme
`)
	host := writeFile(t, root, "app.yaml", `include:
  - { file: ./modules/m.yaml, prefix: /auth }
`)
	cfg := loadFrom(t, host)
	if err := materializeRoutes(cfg, nil); err != nil {
		t.Fatal(err)
	}
	byPath := map[string]*Route{}
	for _, r := range cfg.Routes {
		byPath[r.Path] = r
	}
	if r := byPath["/auth/login"]; r == nil ||
		r.AuthLoginConfig.RedirectOnSuccess != "/auth/home" ||
		r.AuthLoginConfig.RedirectOnFailure != "/auth/login?err=1" {
		t.Errorf("auth-login redirects not prefixed: %+v", r)
	}
	if r := byPath["/auth/signup"]; r == nil ||
		r.AuthSignupConfig.RedirectOnSuccess != "/auth/welcome" ||
		r.AuthSignupConfig.RedirectOnFailure != "/auth/signup?err=1" {
		t.Errorf("auth-signup redirects not prefixed: %+v", r)
	}
	if r := byPath["/auth/logout"]; r == nil ||
		r.AuthLogoutConfig.RedirectOnSuccess != "/auth/bye" ||
		r.AuthLogoutConfig.RedirectOnFailure != "/auth/oops" {
		t.Errorf("auth-logout redirects not prefixed: %+v", r)
	}
	if r := byPath["/auth/magic/req"]; r == nil ||
		r.MagicLinkRequestConfig.CallbackURL != "/auth/magic/verify" {
		t.Errorf("magic-link-request callback not prefixed: %+v", r)
	}
	if r := byPath["/auth/magic/consume"]; r == nil ||
		r.MagicLinkConsumeConfig.SuccessRedirect != "/auth/dashboard" {
		t.Errorf("magic-link-consume redirect not prefixed: %+v", r)
	}
	// Outbound forward_url must NOT be rewritten.
	if r := byPath["/auth/fwd"]; r == nil ||
		r.ForwardConfig.ForwardURL != "http://upstream.local/keepme" {
		t.Errorf("outbound forward_url was rewritten: %+v", r)
	}
}

// ---- standalone extern resolution + dedup + library reject ---------

func setupSharedLibs(t *testing.T, root string) {
	writeFile(t, root, "shared/app-db.yaml", `kind: storage
db: { type: sqlite, path: ./data/app.db }
`)
	writeFile(t, root, "shared/llm.yaml", `kind: plugins
gemini: { transport: http, address: "http://localhost:9000" }
`)
	writeFile(t, root, "shared/events.yaml", `kind: connections
feed: { type: sse, subscribe_path: /events/feed, buffer_size: 16 }
`)
}

func TestStandaloneExternResolves(t *testing.T) {
	root := t.TempDir()
	setupSharedLibs(t, root)
	mod := writeFile(t, root, "modules/rss.yaml", `storage:
  db: { extern: ../shared/app-db.yaml }
plugins:
  gemini: { extern: ../shared/llm.yaml }
connections:
  feed: { extern: ../shared/events.yaml }
routes:
  - { path: /feeds, method: GET, type: content, content: { body: "ok" } }
`)
	cfg := loadFrom(t, mod)
	if cfg.Storage["db"] == nil || cfg.Storage["db"].Type != "sqlite" {
		t.Fatalf("db not resolved: %+v", cfg.Storage)
	}
	if cfg.Plugins["gemini"] == nil || cfg.Plugins["gemini"].Transport != "http" {
		t.Fatalf("gemini not resolved: %+v", cfg.Plugins)
	}
	// borrowed connection keeps canonical subscribe_path
	if cfg.Connections["feed"] == nil || cfg.Connections["feed"].SubscribePath != "/events/feed" {
		t.Fatalf("feed subscribe path = %+v", cfg.Connections["feed"])
	}
}

func TestExternKindMismatch(t *testing.T) {
	root := t.TempDir()
	setupSharedLibs(t, root)
	mod := writeFile(t, root, "modules/bad.yaml", `storage:
  db: { extern: ../shared/llm.yaml }
`)
	_, err := loadConfig(mod)
	if err == nil || !strings.Contains(err.Error(), "kind:plugins library") {
		t.Fatalf("kind mismatch: got %v", err)
	}
}

func TestExternMissingName(t *testing.T) {
	root := t.TempDir()
	setupSharedLibs(t, root)
	mod := writeFile(t, root, "modules/bad.yaml", `storage:
  nope: { extern: ../shared/app-db.yaml }
`)
	_, err := loadConfig(mod)
	if err == nil || !strings.Contains(err.Error(), `has no "nope" entry`) {
		t.Fatalf("missing name: got %v", err)
	}
}

func TestExternMissingFile(t *testing.T) {
	root := t.TempDir()
	mod := writeFile(t, root, "modules/bad.yaml", `storage:
  db: { extern: ../shared/ghost.yaml }
`)
	_, err := loadConfig(mod)
	if err == nil || !strings.Contains(err.Error(), "cannot read library file") {
		t.Fatalf("missing file: got %v", err)
	}
}

func TestExternPlusSiblings(t *testing.T) {
	root := t.TempDir()
	setupSharedLibs(t, root)
	mod := writeFile(t, root, "modules/bad.yaml", `storage:
  db: { extern: ../shared/app-db.yaml, type: sqlite }
`)
	_, err := loadConfig(mod)
	if err == nil || !strings.Contains(err.Error(), "must not also define inline fields") {
		t.Fatalf("extern+siblings: got %v", err)
	}
}

func TestLibraryAsServerRejected(t *testing.T) {
	root := t.TempDir()
	setupSharedLibs(t, root)
	_, err := loadConfig(filepath.Join(root, "shared/app-db.yaml"))
	if err == nil || !strings.Contains(err.Error(), "is a kind:storage library, not a server") {
		t.Fatalf("library-as-server: got %v", err)
	}
}

// ---- identity dedup -------------------------------------------------

func TestDedup_SameLibManyTimesOK(t *testing.T) {
	root := t.TempDir()
	setupSharedLibs(t, root)
	writeFile(t, root, "modules/a.yaml", `storage: { db: { extern: ../shared/app-db.yaml } }
routes: [ { path: /a, method: GET, type: content, content: { body: a } } ]
`)
	writeFile(t, root, "modules/b.yaml", `storage: { db: { extern: ../shared/app-db.yaml } }
routes: [ { path: /b, method: GET, type: content, content: { body: b } } ]
`)
	host := writeFile(t, root, "app.yaml", `include:
  - { file: ./modules/a.yaml }
  - { file: ./modules/b.yaml }
`)
	cfg := loadFrom(t, host)
	if len(cfg.Storage) != 1 || cfg.Storage["db"] == nil {
		t.Fatalf("expected one deduped db, got %+v", cfg.Storage)
	}
}

func TestDedup_TwoAuthorsConflict(t *testing.T) {
	root := t.TempDir()
	setupSharedLibs(t, root)
	// module a externs db; module b authors its own db (identical body)
	writeFile(t, root, "modules/a.yaml", `storage: { db: { extern: ../shared/app-db.yaml } }
`)
	writeFile(t, root, "modules/b.yaml", `storage:
  db: { type: sqlite, path: ./data/app.db }
`)
	host := writeFile(t, root, "app.yaml", `include:
  - { file: ./modules/a.yaml }
  - { file: ./modules/b.yaml }
`)
	_, err := loadConfig(host)
	if err == nil || !strings.Contains(err.Error(), "authored by two files") {
		t.Fatalf("two-author conflict: got %v", err)
	}
}

// ---- include merge + prefixing + absolute --------------------------

func TestIncludePrefixingAndAbsolute(t *testing.T) {
	root := t.TempDir()
	setupSharedLibs(t, root)
	writeFile(t, root, "modules/rss.yaml", `storage: { db: { extern: ../shared/app-db.yaml } }
connections: { feed: { extern: ../shared/events.yaml } }
routes:
  - { path: /feeds, method: GET, type: content, content: { body: ok } }
`)
	writeFile(t, root, "modules/auth-routes.yaml", `routes:
  - { path: /login, method: GET, type: content, content: { body: login } }
  - { path: /oauth/callback, method: GET, type: content, absolute: true, content: { body: cb } }
`)
	host := writeFile(t, root, "app.yaml", `default: { port: 9939 }
include:
  - { file: ./modules/auth-routes.yaml }
  - { file: ./modules/rss.yaml, prefix: /rss }
`)
	cfg := loadFrom(t, host)
	if err := materializeRoutes(cfg, nil); err != nil {
		t.Fatalf("materialize: %v", err)
	}
	paths := map[string]bool{}
	for _, r := range cfg.Routes {
		paths[r.Path] = true
	}
	for _, want := range []string{"/login", "/oauth/callback", "/rss/feeds"} {
		if !paths[want] {
			t.Errorf("missing route %q (have %v)", want, paths)
		}
	}
	if paths["/rss/oauth/callback"] {
		t.Error("absolute:true route was incorrectly prefixed")
	}
	// borrowed connection keeps canonical path even under prefix
	if cfg.Connections["feed"].SubscribePath != "/events/feed" {
		t.Errorf("borrowed feed subscribe_path moved: %q", cfg.Connections["feed"].SubscribePath)
	}
}

// ---- cycle + depth --------------------------------------------------

func TestIncludeCycle(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "a.yaml", `include: [ { file: ./b.yaml } ]
`)
	writeFile(t, root, "b.yaml", `include: [ { file: ./a.yaml } ]
`)
	_, err := loadConfig(filepath.Join(root, "a.yaml"))
	if err == nil || !strings.Contains(err.Error(), "include cycle detected") {
		t.Fatalf("cycle: got %v", err)
	}
}

func TestIncludeDepthLimit(t *testing.T) {
	root := t.TempDir()
	// chain of 40 files each including the next
	for i := 0; i < 40; i++ {
		next := fmt.Sprintf("m%d.yaml", i+1)
		writeFile(t, root, fmt.Sprintf("m%d.yaml", i), fmt.Sprintf("include: [ { file: ./%s } ]\n", next))
	}
	writeFile(t, root, "m40.yaml", "routes: []\n")
	_, err := loadConfig(filepath.Join(root, "m0.yaml"))
	if err == nil || !strings.Contains(err.Error(), "include depth exceeds 32") {
		t.Fatalf("depth: got %v", err)
	}
}

// ---- included module declares args/env -----------------------------

func TestIncludedModuleArgsRejected(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "mod.yaml", `args: { x: { default: "1" } }
routes: []
`)
	host := writeFile(t, root, "app.yaml", `include: [ { file: ./mod.yaml } ]
`)
	_, err := loadConfig(host)
	if err == nil || !strings.Contains(err.Error(), "must not declare `args:`/`env:`") {
		t.Fatalf("included args: got %v", err)
	}
}

// ---- routesMaterialized convergence --------------------------------

func TestRoutesMaterializedConvergence(t *testing.T) {
	root := t.TempDir()
	setupSharedLibs(t, root)
	writeFile(t, root, "modules/rss.yaml", `routes:
  - { path: /feeds, method: GET, type: content, content: { body: ok } }
`)
	host := writeFile(t, root, "app.yaml", `include:
  - { file: ./modules/rss.yaml, prefix: /rss }
`)
	cfg := loadFrom(t, host)
	s := &Server{Config: cfg}
	rows, err := s.RouteSummaries()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, r := range rows {
		if r.Path == "/rss/feeds" {
			found = true
		}
	}
	if !found {
		t.Fatalf("RouteSummaries did not see merged+prefixed route: %+v", rows)
	}
	// validate path also converges (no double materialization)
	if err := s.ValidateConfig(); err != nil {
		t.Fatalf("validate: %v", err)
	}
	if len(cfg.Routes) != 1 {
		t.Fatalf("expected 1 materialized route, got %d (double-materialized?)", len(cfg.Routes))
	}
}

// ---- JSON / YAML interchange ---------------------------------------

func TestJSONModuleExternsYAMLLib(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "shared/app-db.yaml", `kind: storage
db: { type: sqlite, path: ./data/app.db }
`)
	// JSON module externs the YAML library
	modJSON := writeFile(t, root, "modules/rss.json",
		`{"storage":{"db":{"extern":"../shared/app-db.yaml"}},"routes":[{"path":"/feeds","method":"GET","type":"content","content":{"body":"ok"}}]}`)
	cfgJSON := loadFrom(t, modJSON)

	// YAML module externs the same library
	modYAML := writeFile(t, root, "modules/rss.yaml", `storage: { db: { extern: ../shared/app-db.yaml } }
routes: [ { path: /feeds, method: GET, type: content, content: { body: ok } } ]
`)
	cfgYAML := loadFrom(t, modYAML)

	if cfgJSON.Storage["db"].Path != cfgYAML.Storage["db"].Path ||
		cfgJSON.Storage["db"].Type != cfgYAML.Storage["db"].Type {
		t.Fatalf("JSON and YAML resolved differently: %+v vs %+v",
			cfgJSON.Storage["db"], cfgYAML.Storage["db"])
	}
}
