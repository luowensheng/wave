package match

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubBuildSubHandler wires BuildSubHandlerFn to return a handler
// whose response body is whatever `routeOrId` is. Useful so tests
// can identify which case fired without a full orchestrator.
func stubBuildSubHandler(t *testing.T) func() {
	t.Helper()
	prev := BuildSubHandlerFn
	BuildSubHandlerFn = func(routeOrId any) (http.HandlerFunc, error) {
		if routeOrId == nil {
			return nil, errors.New("nil route")
		}
		label := fmt.Sprintf("%v", routeOrId)
		return func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, label)
		}, nil
	}
	return func() { BuildSubHandlerFn = prev }
}

// hit builds a handler from cfg, fires one request, returns body+status.
func hit(t *testing.T, cfg *Config, req *http.Request) (string, int) {
	t.Helper()
	h, err := cfg.CreateRoute("", "", nil)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}
	rr := httptest.NewRecorder()
	h(rr, req)
	return strings.TrimSpace(rr.Body.String()), rr.Code
}

func TestMatch_MethodEquals(t *testing.T) {
	defer stubBuildSubHandler(t)()
	cfg := &Config{
		Cases: []Case{
			{When: "method", Match: "POST", Route: "post-route"},
			{When: "method", Match: "GET", Route: "get-route"},
		},
		Default: &Case{Route: "default-route"},
	}
	for _, tc := range []struct {
		method, want string
	}{
		{"GET", "get-route"},
		{"POST", "post-route"},
		{"DELETE", "default-route"},
		// case-insensitive on method
		{"post", "post-route"},
	} {
		t.Run(tc.method, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "/", nil)
			body, code := hit(t, cfg, req)
			if code != http.StatusOK || body != tc.want {
				t.Fatalf("method=%s got %d %q, want 200 %q", tc.method, code, body, tc.want)
			}
		})
	}
}

func TestMatch_HeaderRegex(t *testing.T) {
	defer stubBuildSubHandler(t)()
	cfg := &Config{
		Cases: []Case{
			{
				When:  "header",
				Match: map[string]any{"user-agent": map[string]any{"regex": "Mobile|Android|iPhone"}},
				Route: "mobile",
			},
		},
		Default: &Case{Route: "default"},
	}
	for _, tc := range []struct {
		ua, want string
	}{
		{"iPhone Safari", "mobile"},
		{"Android 13", "mobile"},
		{"Mozilla/5.0 (X11; Linux)", "default"},
	} {
		t.Run(tc.ua, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/", nil)
			req.Header.Set("User-Agent", tc.ua)
			body, _ := hit(t, cfg, req)
			if body != tc.want {
				t.Fatalf("ua=%q got %q want %q", tc.ua, body, tc.want)
			}
		})
	}
}

func TestMatch_CookieEqualsAndExists(t *testing.T) {
	defer stubBuildSubHandler(t)()
	tru := true
	cfg := &Config{
		Cases: []Case{
			{When: "cookie", Match: map[string]any{"variant": "beta"}, Route: "beta"},
			{When: "cookie", Match: map[string]any{"session": map[string]any{"exists": tru}}, Route: "authed"},
		},
		Default: &Case{Route: "anon"},
	}

	// no cookies → default
	if got, _ := hit(t, cfg, httptest.NewRequest("GET", "/", nil)); got != "anon" {
		t.Fatalf("no-cookies: got %q", got)
	}

	// beta variant
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "variant", Value: "beta"})
	if got, _ := hit(t, cfg, r); got != "beta" {
		t.Fatalf("beta-variant: got %q", got)
	}

	// non-beta variant + session present → authed
	r = httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: "variant", Value: "alpha"})
	r.AddCookie(&http.Cookie{Name: "session", Value: "xyz"})
	if got, _ := hit(t, cfg, r); got != "authed" {
		t.Fatalf("session-present: got %q", got)
	}
}

func TestMatch_QueryPrefix(t *testing.T) {
	defer stubBuildSubHandler(t)()
	cfg := &Config{
		Cases: []Case{
			{When: "query", Match: map[string]any{"lang": map[string]any{"prefix": "es"}}, Route: "es"},
		},
		Default: &Case{Route: "en"},
	}
	for _, tc := range []struct{ url, want string }{
		{"/?lang=es", "es"},
		{"/?lang=es-MX", "es"},
		{"/?lang=en", "en"},
		{"/", "en"},
	} {
		req := httptest.NewRequest("GET", tc.url, nil)
		if got, _ := hit(t, cfg, req); got != tc.want {
			t.Fatalf("url=%s got %q want %q", tc.url, got, tc.want)
		}
	}
}

func TestMatch_HostEquals(t *testing.T) {
	defer stubBuildSubHandler(t)()
	cfg := &Config{
		Cases: []Case{
			{When: "host", Match: "api.example.com", Route: "api"},
		},
		Default: &Case{Route: "other"},
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Host = "api.example.com"
	if got, _ := hit(t, cfg, r); got != "api" {
		t.Fatal("api host should match")
	}
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Host = "www.example.com"
	if got, _ := hit(t, cfg, r2); got != "other" {
		t.Fatal("non-api host should fall to default")
	}
}

func TestMatch_FirstWins(t *testing.T) {
	defer stubBuildSubHandler(t)()
	cfg := &Config{
		Cases: []Case{
			{When: "method", Match: "GET", Route: "first"},
			{When: "method", Match: "GET", Route: "second"},
		},
	}
	req := httptest.NewRequest("GET", "/", nil)
	if got, _ := hit(t, cfg, req); got != "first" {
		t.Fatalf("first-match-wins violated: got %q", got)
	}
}

func TestMatch_NoMatchNoDefault_404(t *testing.T) {
	defer stubBuildSubHandler(t)()
	cfg := &Config{
		Cases: []Case{
			{When: "method", Match: "POST", Route: "p"},
		},
	}
	req := httptest.NewRequest("GET", "/", nil)
	_, code := hit(t, cfg, req)
	if code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", code)
	}
}

func TestMatch_InvalidRegex_BootFails(t *testing.T) {
	defer stubBuildSubHandler(t)()
	cfg := &Config{
		Cases: []Case{
			{When: "header", Match: map[string]any{"x": map[string]any{"regex": "(unclosed"}}, Route: "x"},
		},
	}
	if _, err := cfg.CreateRoute("", "", nil); err == nil {
		t.Fatal("expected boot error on invalid regex")
	}
}

func TestMatch_UnknownDimension_BootFails(t *testing.T) {
	defer stubBuildSubHandler(t)()
	cfg := &Config{
		Cases: []Case{{When: "fingerprint", Match: "x", Route: "x"}},
	}
	if _, err := cfg.CreateRoute("", "", nil); err == nil {
		t.Fatal("expected boot error on unknown dimension")
	}
}

func TestMatch_EmptyConfig_BootFails(t *testing.T) {
	defer stubBuildSubHandler(t)()
	cfg := &Config{}
	if _, err := cfg.CreateRoute("", "", nil); err == nil {
		t.Fatal("expected boot error on empty config")
	}
}

func TestMatch_NoBuildFn_BootFails(t *testing.T) {
	prev := BuildSubHandlerFn
	BuildSubHandlerFn = nil
	defer func() { BuildSubHandlerFn = prev }()
	cfg := &Config{Cases: []Case{{When: "method", Match: "GET", Route: "x"}}}
	if _, err := cfg.CreateRoute("", "", nil); err == nil {
		t.Fatal("expected boot error when BuildSubHandlerFn is not wired")
	}
}

func TestMatch_KeyedAND(t *testing.T) {
	defer stubBuildSubHandler(t)()
	cfg := &Config{
		Cases: []Case{
			{
				When: "header",
				Match: map[string]any{
					"x-tenant":  "acme",
					"x-feature": map[string]any{"exists": true},
				},
				Route: "acme-feature",
			},
		},
		Default: &Case{Route: "other"},
	}
	// Both set → match
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Tenant", "acme")
	r.Header.Set("X-Feature", "on")
	if got, _ := hit(t, cfg, r); got != "acme-feature" {
		t.Fatalf("both-set: got %q", got)
	}
	// Only tenant → no match (feature missing)
	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Tenant", "acme")
	if got, _ := hit(t, cfg, r); got != "other" {
		t.Fatalf("only-tenant: got %q", got)
	}
	// Tenant wrong → no match
	r = httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Tenant", "globex")
	r.Header.Set("X-Feature", "on")
	if got, _ := hit(t, cfg, r); got != "other" {
		t.Fatalf("wrong-tenant: got %q", got)
	}
}
