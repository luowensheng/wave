package servers

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/luowensheng/wave/usecases/routes"
)

func TestNotFoundServesConfiguredFile(t *testing.T) {
	dir := t.TempDir()
	page := filepath.Join(dir, "404.html")
	if err := os.WriteFile(page, []byte("<h1>missing</h1>"), 0o644); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	s := &Server{
		mux: mux,
		Config: &Config{
			Routes: []*Route{},
			NotFound: &Route{
				Type:       "file",
				FileConfig: &routes.FileConfig{FilePath: page},
			},
		},
	}
	if err := s.registerNotFound(nil); err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/no-such-thing", nil))
	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
	if got := w.Body.String(); got == "" {
		t.Errorf("empty body")
	}
}

func TestNotFoundDoesNotShadowSpecificRoutes(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/known", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = w.Write([]byte("hi"))
	})

	dir := t.TempDir()
	page := filepath.Join(dir, "404.html")
	_ = os.WriteFile(page, []byte("missing"), 0o644)
	s := &Server{
		mux: mux,
		Config: &Config{
			NotFound: &Route{Type: "file", FileConfig: &routes.FileConfig{FilePath: page}},
		},
	}
	if err := s.registerNotFound(nil); err != nil {
		t.Fatal(err)
	}

	// Specific path still returns 200.
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/known", nil))
	if w.Code != 200 || w.Body.String() != "hi" {
		t.Errorf("known route hijacked: status=%d body=%q", w.Code, w.Body.String())
	}

	// Unknown path → 404 + branded body.
	w2 := httptest.NewRecorder()
	mux.ServeHTTP(w2, httptest.NewRequest("GET", "/totally-unknown", nil))
	if w2.Code != http.StatusNotFound {
		t.Errorf("unknown route status = %d", w2.Code)
	}
}

func TestNotFoundNilSkipsRegistration(t *testing.T) {
	mux := http.NewServeMux()
	s := &Server{mux: mux, Config: &Config{}}
	if err := s.registerNotFound(nil); err != nil {
		t.Errorf("nil NotFound should be a no-op: %v", err)
	}
}
