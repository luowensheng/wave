package studio

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// helper: spin up a fake "wave" target HTTP server, write a
// project's server.yaml pointing at it, register, and return the
// configured api handler ready for httptest.
func setupAPI(t *testing.T) (*Registry, *Supervisor, http.Handler, *Project, *httptest.Server) {
	t.Helper()
	// fake target server (stands in for the project's running wave)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/metrics":
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintln(w, `# HELP fake metrics`)
			fmt.Fprintln(w, `wave_http_requests_total{method="GET",status="200"} 7`)
		case "/echo":
			w.Header().Set("X-Echo", "yes")
			w.WriteHeader(201)
			body, _ := io.ReadAll(r.Body)
			fmt.Fprintf(w, "method=%s body=%s", r.Method, string(body))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(target.Close)

	u, _ := url.Parse(target.URL)
	host, portStr, _ := strings.Cut(u.Host, ":")
	port, _ := strconv.Atoi(portStr)

	projDir := t.TempDir()
	yaml := fmt.Sprintf(`name: testproj
default:
  host: %s
  port: %d
routes:
  - path: /echo
    method: POST
    type: forward
    forward:
      url: irrelevant
    description: echoes back
  - path: /
    method: GET
    type: file
`, host, port)
	if err := os.WriteFile(filepath.Join(projDir, "server.yaml"), []byte(yaml), 0o644); err != nil {
		t.Fatal(err)
	}

	dataDir := t.TempDir()
	reg, err := LoadRegistry(dataDir)
	if err != nil {
		t.Fatal(err)
	}
	p, err := reg.Add(projDir, "server.yaml")
	if err != nil {
		t.Fatal(err)
	}
	sup := NewSupervisor("/bin/true")
	return reg, sup, apiHandler(reg, sup), p, target
}

func TestAPIListProjects(t *testing.T) {
	_, _, h, p, _ := setupAPI(t)
	r := httptest.NewRequest("GET", "/api/projects", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("code %d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Projects []map[string]any `json:"projects"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.Projects) != 1 {
		t.Fatalf("want 1 project, got %d", len(out.Projects))
	}
	if out.Projects[0]["id"] != p.ID {
		t.Errorf("project id mismatch")
	}
}

func TestAPIRoutesParsed(t *testing.T) {
	_, _, h, p, _ := setupAPI(t)
	r := httptest.NewRequest("GET", "/api/projects/"+p.ID+"/routes", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("code %d body=%s", w.Code, w.Body.String())
	}
	var out struct {
		Host   string         `json:"host"`
		Port   int            `json:"port"`
		Routes []routeSummary `json:"routes"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	if len(out.Routes) != 2 {
		t.Fatalf("want 2 routes, got %d (%+v)", len(out.Routes), out.Routes)
	}
	want := map[string]string{"/echo": "forward", "/": "file"}
	for _, r := range out.Routes {
		if want[r.Path] != r.Type {
			t.Errorf("route %s: type=%s want %s", r.Path, r.Type, want[r.Path])
		}
	}
}

func TestAPITestRouteProxies(t *testing.T) {
	_, _, h, p, _ := setupAPI(t)
	body := bytes.NewBufferString(`{"method":"POST","path":"/echo","body":"hi"}`)
	r := httptest.NewRequest("POST", "/api/projects/"+p.ID+"/test-route", body)
	r.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("code %d body=%s", w.Code, w.Body.String())
	}
	var resp proxyResponse
	_ = json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Status != 201 {
		t.Errorf("proxied status: got %d want 201", resp.Status)
	}
	if !strings.Contains(resp.Body, "method=POST body=hi") {
		t.Errorf("unexpected proxied body: %q", resp.Body)
	}
	if got := resp.Headers["X-Echo"]; len(got) == 0 || got[0] != "yes" {
		t.Errorf("missing X-Echo header: %+v", resp.Headers)
	}
}

func TestAPIMetricsProxy(t *testing.T) {
	_, _, h, p, _ := setupAPI(t)
	r := httptest.NewRequest("GET", "/api/projects/"+p.ID+"/metrics", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("code %d body=%s", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), "wave_http_requests_total") {
		t.Errorf("expected counter in proxied metrics: %s", w.Body.String())
	}
}

func TestAPIStatusUnknownProject(t *testing.T) {
	_, _, h, _, _ := setupAPI(t)
	r := httptest.NewRequest("GET", "/api/projects/does-not-exist/status", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Errorf("got %d want 404", w.Code)
	}
}

func TestAPIDelete(t *testing.T) {
	reg, _, h, p, _ := setupAPI(t)
	r := httptest.NewRequest("DELETE", "/api/projects/"+p.ID, nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("code %d", w.Code)
	}
	if got := len(reg.List()); got != 0 {
		t.Errorf("project not removed; have %d", got)
	}
}

func TestAPIAuthRequired(t *testing.T) {
	dataDir := t.TempDir()
	reg, _ := LoadRegistry(dataDir)
	sup := NewSupervisor("/bin/true")
	tok, _ := loadOrCreateToken(dataDir)
	mux := http.NewServeMux()
	mux.Handle("/api/", authMiddleware(tok, apiHandler(reg, sup)))

	r := httptest.NewRequest("GET", "/api/projects", nil)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("expected 401 without token, got %d", w.Code)
	}

	r = httptest.NewRequest("GET", "/api/projects", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w = httptest.NewRecorder()
	mux.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("expected 200 with token, got %d body=%s", w.Code, w.Body.String())
	}
}
