package studio

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateTokenIdempotent(t *testing.T) {
	dir := t.TempDir()
	a, err := loadOrCreateToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(a) < 32 {
		t.Errorf("token too short: %q", a)
	}
	b, err := loadOrCreateToken(dir)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("token must be stable across reads: %q vs %q", a, b)
	}
	st, err := os.Stat(filepath.Join(dir, "studio.token"))
	if err != nil {
		t.Fatal(err)
	}
	// On unix, expect 0600. On windows, mode bits aren't meaningful.
	if st.Mode().Perm()&0o077 != 0 {
		t.Errorf("token file permissive: %v", st.Mode())
	}
}

func TestAuthMiddleware(t *testing.T) {
	const tok = "abcdef0123456789"
	h := authMiddleware(tok, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	}))

	cases := []struct {
		name string
		mod  func(*http.Request)
		code int
	}{
		{"no auth", func(r *http.Request) {}, 401},
		{"wrong bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer nope") }, 401},
		{"good bearer", func(r *http.Request) { r.Header.Set("Authorization", "Bearer "+tok) }, 204},
		{"good cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "studio_token", Value: tok}) }, 204},
		{"wrong cookie", func(r *http.Request) { r.AddCookie(&http.Cookie{Name: "studio_token", Value: "x"}) }, 401},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := httptest.NewRequest("GET", "/api/projects", nil)
			c.mod(r)
			w := httptest.NewRecorder()
			h.ServeHTTP(w, r)
			if w.Code != c.code {
				t.Errorf("got %d want %d", w.Code, c.code)
			}
		})
	}
}
