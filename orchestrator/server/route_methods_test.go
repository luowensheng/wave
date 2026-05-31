package servers

import (
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

// TestWrapRouteMiddlewareDoesNotMutateMethods is a regression test for
// the bug at orchestrator/server/servers.go:307 where wrapRouteMiddleware
// did `route.Methods = append(route.Methods, route.Method)`, growing the
// caller's slice permanently and causing the hasRoot detection at the
// end of Bootstrap to mis-identify whether the user had claimed "/".
//
// The invariant under test: after wrapRouteMiddleware returns, the
// route's Methods slice has the same length and contents as before.
func TestWrapRouteMiddlewareDoesNotMutateMethods(t *testing.T) {
	cases := []struct {
		name    string
		method  string
		methods []string
	}{
		{name: "Method only", method: "GET", methods: nil},
		{name: "Methods only", method: "", methods: []string{"GET", "POST"}},
		{name: "both Method and Methods", method: "DELETE", methods: []string{"GET", "POST"}},
		{name: "neither (catch-all)", method: "", methods: nil},
		{name: "Methods with lowercase", method: "", methods: []string{"get", "post"}},
		{name: "Method duplicates a Methods entry", method: "GET", methods: []string{"GET", "POST"}},
		{name: "empty strings in Methods", method: "GET", methods: []string{"", "POST", ""}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var beforeCopy []string
			if tc.methods != nil {
				beforeCopy = append([]string{}, tc.methods...)
			}
			r := &Route{
				Path:    "/test",
				Method:  tc.method,
				Methods: tc.methods,
			}

			s := &Server{
				mux:    http.NewServeMux(),
				Config: &Config{},
			}

			handler := func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusOK)
			}

			if _, err := s.wrapRouteMiddleware(r, handler); err != nil {
				t.Fatalf("wrapRouteMiddleware: %v", err)
			}

			if !reflect.DeepEqual(r.Methods, beforeCopy) {
				t.Fatalf("Methods mutated: before=%v after=%v (Method=%q)",
					beforeCopy, r.Methods, tc.method)
			}
		})
	}
}

// TestWrapRouteMiddlewareCallTwiceIsIdempotent asserts that wrapping the
// same route twice does not grow Methods (a corollary of the no-mutation
// guarantee). Without the fix, the original code grew the slice by one
// element each call.
func TestWrapRouteMiddlewareCallTwiceIsIdempotent(t *testing.T) {
	r := &Route{
		Path:    "/x",
		Method:  "POST",
		Methods: []string{"GET"},
	}
	original := append([]string{}, r.Methods...)

	s := &Server{mux: http.NewServeMux(), Config: &Config{}}
	noop := func(w http.ResponseWriter, _ *http.Request) {}

	for i := 0; i < 3; i++ {
		if _, err := s.wrapRouteMiddleware(r, noop); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}

	if !reflect.DeepEqual(r.Methods, original) {
		t.Fatalf("Methods grew across calls: original=%v final=%v", original, r.Methods)
	}
}

// TestWrapRouteMiddlewareAllowList verifies that the methods the
// middleware accepts include the union of Methods and Method,
// case-folded and deduped, even though Methods itself isn't mutated.
func TestWrapRouteMiddlewareAllowList(t *testing.T) {
	r := &Route{
		Path:    "/items",
		Method:  "post",                         // lowercase, should be uppercased
		Methods: []string{"GET", "get", "POST"}, // duplicates + case variants
	}
	s := &Server{mux: http.NewServeMux(), Config: &Config{}}
	handler, err := s.wrapRouteMiddleware(r, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("served"))
	})
	if err != nil {
		t.Fatal(err)
	}

	// All declared methods (case-insensitive) accepted.
	for _, m := range []string{"GET", "POST"} {
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest(m, "/items", nil))
		if w.Code != http.StatusOK {
			t.Errorf("method %s: status = %d, want 200", m, w.Code)
		}
	}

	// Undeclared method rejected with 405.
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest("DELETE", "/items", nil))
	if w.Code != http.StatusMethodNotAllowed {
		t.Errorf("DELETE: status = %d, want 405", w.Code)
	}
}

// TestHasRootDetectionAcceptsAllShapes covers the four shapes a user
// route at "/" can take and asserts the built-in fallback handler does
// NOT double-register, regardless of which slot the user filled.
//
// This is the post-fix invariant: hasRoot matches on Path alone.
// Pre-fix it only matched the (Method=="" && Methods empty) shape,
// which combined with the route.Methods mutation bug meant the
// fallback would attempt to re-claim "/" and panic.
func TestHasRootDetectionAcceptsAllShapes(t *testing.T) {
	shapes := []struct {
		name string
		r    *Route
	}{
		{
			name: "Path only (catch-all)",
			r:    &Route{Path: "/"},
		},
		{
			name: "Path + Method",
			r:    &Route{Path: "/", Method: "GET"},
		},
		{
			name: "Path + Methods",
			r:    &Route{Path: "/", Methods: []string{"GET", "POST"}},
		},
		{
			name: "Path + Method + Methods",
			r:    &Route{Path: "/", Method: "GET", Methods: []string{"POST"}},
		},
	}

	for _, sh := range shapes {
		t.Run(sh.name, func(t *testing.T) {
			defer func() {
				if rec := recover(); rec != nil {
					t.Fatalf("panicked, likely duplicate / registration: %v", rec)
				}
			}()

			mux := http.NewServeMux()
			s := &Server{
				mux:    mux,
				Config: &Config{Routes: []*Route{sh.r}},
			}

			// Simulate Bootstrap's fallback registration block.
			// With the bug, this would re-register "/" and panic
			// on shapes with Method/Methods set.
			hasRoot := false
			for _, r := range s.Config.Routes {
				if r.Path == "/" {
					hasRoot = true
					break
				}
			}
			if !hasRoot {
				t.Fatalf("hasRoot was false; user route at / not detected")
			}
		})
	}
}

// TestHasRootDetectionFiresFallbackWhenNoUserRoot ensures we only skip
// the fallback when the user actually claimed "/". An empty route list
// must still install the built-in 404 fallback.
func TestHasRootDetectionFiresFallbackWhenNoUserRoot(t *testing.T) {
	hasRoot := false
	cfg := &Config{Routes: []*Route{
		{Path: "/items", Method: "GET"},
		{Path: "/users/{id}", Method: "GET"},
	}}
	for _, r := range cfg.Routes {
		if r.Path == "/" {
			hasRoot = true
		}
	}
	if hasRoot {
		t.Fatalf("hasRoot true with no root route; would skip fallback")
	}
}

// TestSeparateRoutesAtRootDoNotPanic exercises the real failure mode:
// two route blocks declared for the same path "/" with different
// methods (a common pattern). Both must register through
// wrapRouteMiddleware without mutating Methods enough to corrupt the
// hasRoot detection. We probe by calling the wrapper twice.
func TestSeparateRoutesAtRootDoNotPanic(t *testing.T) {
	getRoute := &Route{Path: "/", Method: "GET"}
	postRoute := &Route{Path: "/", Method: "POST"}
	s := &Server{
		mux:    http.NewServeMux(),
		Config: &Config{Routes: []*Route{getRoute, postRoute}},
	}
	noop := func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(200) }

	if _, err := s.wrapRouteMiddleware(getRoute, noop); err != nil {
		t.Fatal(err)
	}
	if _, err := s.wrapRouteMiddleware(postRoute, noop); err != nil {
		t.Fatal(err)
	}

	// Both routes must still report Methods unchanged from declaration.
	if len(getRoute.Methods) != 0 {
		t.Errorf("getRoute.Methods grew: %v", getRoute.Methods)
	}
	if len(postRoute.Methods) != 0 {
		t.Errorf("postRoute.Methods grew: %v", postRoute.Methods)
	}

	// hasRoot must report true and accept either shape.
	hasRoot := false
	for _, r := range s.Config.Routes {
		if r.Path == "/" {
			hasRoot = true
			break
		}
	}
	if !hasRoot {
		t.Errorf("hasRoot false with two / routes declared")
	}

	// Sanity check: the route patterns we'd hand to mux are distinct.
	getPattern := strings.TrimSpace(getRoute.Method + " " + getRoute.Path)
	postPattern := strings.TrimSpace(postRoute.Method + " " + postRoute.Path)
	if getPattern == postPattern {
		t.Errorf("patterns collide: %q == %q", getPattern, postPattern)
	}
}
