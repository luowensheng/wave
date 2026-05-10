package forward

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestExtractPlaceholders(t *testing.T) {
	cases := []struct {
		in   string
		want []string
	}{
		{"https://api.example.com/v2/items/{id}", []string{"id"}},
		{"https://x/{a}/{b}/{a}", []string{"a", "b"}}, // dedupe
		{"https://x/no-template", nil},
		{"https://x/{}", nil}, // empty key skipped
	}
	for _, c := range cases {
		got := extractPlaceholders(c.in)
		if !equal(got, c.want) {
			t.Errorf("extractPlaceholders(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestForwardTemplatedURL(t *testing.T) {
	// Upstream that asserts the resolved path includes the substituted id.
	var lastPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
		_, _ = io.WriteString(w, "ok")
	}))
	defer upstream.Close()

	cfg := &Config{ForwardURL: upstream.URL + "/v2/items/{id}/details"}
	h, err := cfg.CreateRoute("GET", "/items/{id}", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Use a real Go 1.22 ServeMux so r.PathValue("id") is populated.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /items/{id}", h)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/items/abc-123", nil)
	mux.ServeHTTP(w, r)

	if w.Code != 200 {
		t.Fatalf("status = %d body = %q", w.Code, w.Body.String())
	}
	if lastPath != "/v2/items/abc-123/details" {
		t.Errorf("upstream got path = %q", lastPath)
	}
}

func TestForwardStaticURLBackwardCompat(t *testing.T) {
	var lastPath string
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		lastPath = r.URL.Path
	}))
	defer upstream.Close()

	cfg := &Config{ForwardURL: upstream.URL + "/api"}
	h, err := cfg.CreateRoute("GET", "/proxy", nil)
	if err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/proxy/", h)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/proxy/things/1", nil)
	mux.ServeHTTP(w, r)

	if lastPath != "/api/things/1" {
		t.Errorf("upstream path = %q", lastPath)
	}
}
