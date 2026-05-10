package servers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFailRendererDefaultJSON(t *testing.T) {
	def := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(413)
		_, _ = w.Write([]byte(`{"error":"too big"}`))
	}
	r, err := compileFailAction(nil, 413, def, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.Render(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 413 || !strings.Contains(w.Body.String(), "too big") {
		t.Errorf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestFailRendererRedirect(t *testing.T) {
	r, err := compileFailAction(&FailAction{Redirect: "/oops"}, 400, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.Render(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/oops" {
		t.Errorf("status=%d loc=%q", w.Code, w.Header().Get("Location"))
	}
}

func TestFailRendererInlineTemplateAndStatus(t *testing.T) {
	r, _ := compileFailAction(&FailAction{
		TemplateInline: "<h1>nope</h1>",
		Status:         418,
		Headers:        map[string]string{"X-Reason": "teapot"},
	}, 400, nil, nil)
	w := httptest.NewRecorder()
	r.Render(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 418 {
		t.Errorf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "nope") {
		t.Errorf("body = %q", w.Body.String())
	}
	if w.Header().Get("X-Reason") != "teapot" {
		t.Errorf("X-Reason = %q", w.Header().Get("X-Reason"))
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/html") {
		t.Errorf("content-type = %q", w.Header().Get("Content-Type"))
	}
}

func TestFailRendererFileTemplate(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "err.txt")
	_ = os.WriteFile(path, []byte("plain failure body"), 0o644)
	r, err := compileFailAction(&FailAction{TemplateFile: path}, 400, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	r.Render(w, httptest.NewRequest("GET", "/", nil))
	if !strings.Contains(w.Body.String(), "plain failure") {
		t.Errorf("body = %q", w.Body.String())
	}
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("content-type = %q", w.Header().Get("Content-Type"))
	}
}

func TestFailRendererRoutePathDelegates(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/errors/inputs", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(422)
		_, _ = w.Write([]byte("delegated"))
	})
	r, _ := compileFailAction(&FailAction{RoutePath: "/errors/inputs"}, 400, nil, mux)
	w := httptest.NewRecorder()
	r.Render(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 422 || w.Body.String() != "delegated" {
		t.Errorf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestFailRendererPrecedenceRoutePathOverInline(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/x", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("router"))
	})
	r, _ := compileFailAction(&FailAction{
		RoutePath:      "/x",
		TemplateInline: "should not see this",
		Redirect:       "/should-not-redirect",
	}, 400, nil, mux)
	w := httptest.NewRecorder()
	r.Render(w, httptest.NewRequest("GET", "/", nil))
	if w.Body.String() != "router" {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestFailRendererMissingFileFailsCompile(t *testing.T) {
	if _, err := compileFailAction(&FailAction{TemplateFile: "/no/such/path.html"}, 500, nil, nil); err == nil {
		t.Error("expected error for missing template file")
	}
}

func TestSwapStatusReplacesMatchingCode(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "blocked", 401)
	})
	called := 0
	swap := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		w.WriteHeader(302)
		w.Header().Set("Location", "/login")
	})
	h := swapStatus(inner, 401, swap)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if called != 1 || w.Code != 302 {
		t.Errorf("called=%d status=%d", called, w.Code)
	}
}

func TestSwapStatusPassesNonMatching(t *testing.T) {
	inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	})
	called := 0
	swap := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
	})
	h := swapStatus(inner, 401, swap)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if called != 0 || w.Code != 200 || w.Body.String() != "ok" {
		t.Errorf("called=%d status=%d body=%q", called, w.Code, w.Body.String())
	}
}
