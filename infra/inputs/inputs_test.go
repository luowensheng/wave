package inputs

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func mustCompile(t *testing.T, specs []Spec) *SpecSet {
	t.Helper()
	set, err := Compile(specs)
	if err != nil {
		t.Fatal(err)
	}
	return set
}

func TestCompileRejectsBadConfig(t *testing.T) {
	cases := [][]Spec{
		{{}}, // missing name
		{{Name: "a"}, {Name: "a"}}, // duplicate
		{{Name: "x", Source: "weird"}},
		{{Name: "x", Type: "weird"}},
		{{Name: "x", Pattern: "[invalid"}},
	}
	for i, c := range cases {
		if _, err := Compile(c); err == nil {
			t.Errorf("case %d should fail", i)
		}
	}
}

func TestParseQueryAndPath(t *testing.T) {
	set := mustCompile(t, []Spec{
		{Name: "id", Source: SourcePath, Type: TypeInt, Required: true},
		{Name: "limit", Source: SourceQuery, Type: TypeInt, Default: int64(10)},
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/items/{id}", func(w http.ResponseWriter, r *http.Request) {
		res := set.Parse(r)
		if len(res.Issues) > 0 {
			t.Fatalf("issues: %+v", res.Issues)
		}
		_ = json.NewEncoder(w).Encode(res.Values)
	})

	w := httptest.NewRecorder()
	mux.ServeHTTP(w, httptest.NewRequest("GET", "/items/42?limit=25", nil))
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var got map[string]any
	_ = json.Unmarshal(w.Body.Bytes(), &got)
	if got["id"].(float64) != 42 || got["limit"].(float64) != 25 {
		t.Errorf("got %+v", got)
	}
}

func TestParseDefaultApplied(t *testing.T) {
	set := mustCompile(t, []Spec{
		{Name: "limit", Source: SourceQuery, Type: TypeInt, Default: int64(10)},
	})
	res := set.Parse(httptest.NewRequest("GET", "/", nil))
	if res.Values["limit"].(int64) != 10 {
		t.Errorf("default not applied: %+v", res.Values)
	}
}

func TestParseRequiredMissing(t *testing.T) {
	set := mustCompile(t, []Spec{
		{Name: "id", Source: SourceQuery, Required: true},
	})
	res := set.Parse(httptest.NewRequest("GET", "/", nil))
	if len(res.Issues) != 1 || res.Issues[0].Reason != "required" {
		t.Errorf("issues = %+v", res.Issues)
	}
}

func TestParseTypeCoercionFails(t *testing.T) {
	set := mustCompile(t, []Spec{
		{Name: "n", Source: SourceQuery, Type: TypeInt},
	})
	res := set.Parse(httptest.NewRequest("GET", "/?n=notint", nil))
	if len(res.Issues) != 1 || !strings.Contains(res.Issues[0].Reason, "integer") {
		t.Errorf("issues = %+v", res.Issues)
	}
}

func TestParseEmailUUID(t *testing.T) {
	set := mustCompile(t, []Spec{
		{Name: "email", Source: SourceQuery, Type: TypeEmail},
		{Name: "id", Source: SourceQuery, Type: TypeUUID},
	})
	res := set.Parse(httptest.NewRequest("GET",
		"/?email=alice@x.io&id=550e8400-e29b-41d4-a716-446655440000", nil))
	if len(res.Issues) > 0 {
		t.Errorf("issues = %+v", res.Issues)
	}

	bad := set.Parse(httptest.NewRequest("GET", "/?email=nope&id=not-a-uuid", nil))
	if len(bad.Issues) != 2 {
		t.Errorf("expected 2 issues, got %+v", bad.Issues)
	}
}

func TestParsePatternAndMinMax(t *testing.T) {
	min := 3.0
	max := 5.0
	set := mustCompile(t, []Spec{
		{Name: "code", Source: SourceQuery, Pattern: `^[A-Z]+$`, Min: &min, Max: &max},
		{Name: "n", Source: SourceQuery, Type: TypeInt, Min: &min, Max: &max},
	})
	bad := set.Parse(httptest.NewRequest("GET", "/?code=ab&n=10", nil))
	if len(bad.Issues) != 2 {
		t.Errorf("expected 2 issues: %+v", bad.Issues)
	}
	good := set.Parse(httptest.NewRequest("GET", "/?code=ABC&n=4", nil))
	if len(good.Issues) > 0 {
		t.Errorf("issues: %+v", good.Issues)
	}
}

func TestParseEnum(t *testing.T) {
	set := mustCompile(t, []Spec{
		{Name: "color", Source: SourceQuery, Enum: []string{"red", "blue"}},
	})
	bad := set.Parse(httptest.NewRequest("GET", "/?color=green", nil))
	if len(bad.Issues) != 1 {
		t.Errorf("issues = %+v", bad.Issues)
	}
	good := set.Parse(httptest.NewRequest("GET", "/?color=red", nil))
	if len(good.Issues) > 0 {
		t.Errorf("issues: %+v", good.Issues)
	}
}

func TestParseBodyAndDownstreamReread(t *testing.T) {
	set := mustCompile(t, []Spec{
		{Name: "name", Source: SourceBody, Required: true},
	})
	r := httptest.NewRequest("POST", "/", strings.NewReader(`{"name":"alice","extra":"unused"}`))
	r.Header.Set("Content-Type", "application/json")
	res := set.Parse(r)
	if res.Values["name"] != "alice" {
		t.Errorf("name = %v", res.Values["name"])
	}
	// Downstream re-read should still work.
	body, _ := io.ReadAll(r.Body)
	if !strings.Contains(string(body), `"alice"`) {
		t.Errorf("body re-read broken: %q", string(body))
	}
}

func TestParseFormHeaderCookie(t *testing.T) {
	set := mustCompile(t, []Spec{
		{Name: "kind", Source: SourceForm, Required: true},
		{Name: "csrf", Source: SourceHeader, From: "X-Csrf-Token", Required: true},
		{Name: "session", Source: SourceCookie, From: "sess", Required: true},
	})
	r := httptest.NewRequest("POST", "/",
		strings.NewReader("kind=foo&other=ignored"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.Header.Set("X-Csrf-Token", "abc")
	r.AddCookie(&http.Cookie{Name: "sess", Value: "xyz"})
	res := set.Parse(r)
	if len(res.Issues) > 0 {
		t.Fatalf("issues: %+v", res.Issues)
	}
	if res.Values["kind"] != "foo" || res.Values["csrf"] != "abc" || res.Values["session"] != "xyz" {
		t.Errorf("values: %+v", res.Values)
	}
}

func TestMiddleware400OnIssues(t *testing.T) {
	set := mustCompile(t, []Spec{
		{Name: "id", Source: SourceQuery, Required: true},
	})
	called := false
	mw := Middleware(set)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusBadRequest || called {
		t.Fatalf("status=%d called=%v", w.Code, called)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp["error"] != "input validation failed" {
		t.Errorf("body: %+v", resp)
	}
	issues := resp["issues"].([]any)
	if len(issues) != 1 {
		t.Errorf("expected 1 issue, got %d", len(issues))
	}
}

func TestMiddleware200WhenValid(t *testing.T) {
	set := mustCompile(t, []Spec{
		{Name: "id", Source: SourceQuery, Type: TypeInt, Required: true},
	})
	mw := Middleware(set)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		v := FromContext(r.Context())
		if v["id"].(int64) != 7 {
			t.Errorf("ctx values: %+v", v)
		}
		w.WriteHeader(200)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/?id=7", nil))
	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}
}

func TestMiddlewareNilSetIsTransparent(t *testing.T) {
	mw := Middleware(nil)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 204 {
		t.Errorf("status = %d", w.Code)
	}
}

func TestNamesPreservesOrder(t *testing.T) {
	set := mustCompile(t, []Spec{{Name: "a"}, {Name: "b"}, {Name: "c"}})
	got := set.Names()
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Errorf("Names() = %v", got)
	}
}
