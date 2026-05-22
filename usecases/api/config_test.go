// Tests for `type: api` — server-orchestrated outbound HTTP. The
// route templates URL + body with request data, calls an upstream,
// and either streams or buffers the response. We stand up a local
// httptest server as the "upstream" so tests are hermetic.
package api

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestAPI_PassesRequestBodyToUpstream(t *testing.T) {
	var receivedBody atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		receivedBody.Store(string(b))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	defer upstream.Close()

	cfg := &Config{
		Request: &Request{
			Method: "POST",
			URL:    upstream.URL + "/v1/users",
			Body:   `{"name":"{{.name}}"}`,
		},
		Response: &Response{},
	}
	h, err := cfg.CreateRoute("POST", "/users", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/users", strings.NewReader(`{"name":"ada"}`))
	req.Header.Set("Content-Type", "application/json")
	h(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", rr.Code)
	}
	if got := receivedBody.Load().(string); got != `{"name":"ada"}` {
		t.Fatalf("upstream got body %q, want %q", got, `{"name":"ada"}`)
	}
}

func TestAPI_URLTemplatedWithRequestData(t *testing.T) {
	var receivedPath atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath.Store(r.URL.Path)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	cfg := &Config{
		Request: &Request{
			Method: "GET",
			URL:    upstream.URL + "/users/{{.id}}/profile",
			Body:   "",
		},
		Response: &Response{},
	}
	h, _ := cfg.CreateRoute("POST", "/proxy", nil)

	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/proxy", strings.NewReader(`{"id":42}`))
	req.Header.Set("Content-Type", "application/json")
	h(rr, req)

	if got := receivedPath.Load().(string); got != "/users/42/profile" {
		t.Fatalf("upstream got path %q", got)
	}
}

func TestAPI_StaticHeadersInjected(t *testing.T) {
	var receivedAuth atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuth.Store(r.Header.Get("X-Static-Token"))
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	cfg := &Config{
		Request: &Request{
			Method: "POST",
			URL:    upstream.URL + "/",
			Body:   "{}",
			Headers: [][2]string{
				{"X-Static-Token", "abc123"},
			},
		},
		Response: &Response{},
	}
	h, _ := cfg.CreateRoute("POST", "/x", nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	h(rr, req)

	if got := receivedAuth.Load().(string); got != "abc123" {
		t.Fatalf("static header lost: got %q", got)
	}
}

func TestAPI_StaticHeaderNotOverwrittenByCaller(t *testing.T) {
	// Config: "X-Static-Token: abc123" is a default — but if the
	// caller already supplied an X-Static-Token, the inner request
	// should not have it overwritten (the implementation only sets
	// the static header when the caller didn't).
	var got atomic.Value
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Store(r.Header.Get("X-Static-Token"))
		w.WriteHeader(200)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer upstream.Close()

	cfg := &Config{
		Request: &Request{
			Method:  "POST",
			URL:     upstream.URL + "/",
			Body:    "{}",
			Headers: [][2]string{{"X-Static-Token", "default-token"}},
		},
		Response: &Response{},
	}
	h, _ := cfg.CreateRoute("POST", "/x", nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x", strings.NewReader(`{}`))
	req.Header.Set("X-Static-Token", "caller-token")
	req.Header.Set("Content-Type", "application/json")
	h(rr, req)

	// Caller's header should pass through and not be replaced by the
	// config default. Note: the request copies all caller headers,
	// so the final value is the caller's.
	if v := got.Load().(string); v != "caller-token" {
		t.Fatalf("caller header should pass through, got %q", v)
	}
	_ = rr
}

func TestAPI_RejectsInvalidJSONBodyWhenNoInputs(t *testing.T) {
	// With no inputs middleware and no inputs in context, the handler
	// JSON-decodes the request body. Malformed JSON → 400.
	cfg := &Config{
		Request: &Request{
			Method: "POST",
			URL:    "http://unused.example/",
			Body:   "{}",
		},
		Response: &Response{},
	}
	h, _ := cfg.CreateRoute("POST", "/x", nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x", strings.NewReader("not-json"))
	h(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestAPI_BadGatewayOnUpstreamUnreachable(t *testing.T) {
	cfg := &Config{
		Request: &Request{
			Method: "POST",
			// 127.0.0.1:1 is well-known unreachable.
			URL:  "http://127.0.0.1:1/",
			Body: "{}",
		},
		Response: &Response{},
	}
	h, _ := cfg.CreateRoute("POST", "/x", nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/x", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	h(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("got %d, want 502", rr.Code)
	}
}

func TestAPI_PassesUpstreamBodyThroughWhenNoTransform(t *testing.T) {
	upstreamBody := `{"users":[{"id":1,"name":"ada"}],"page":1}`
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		_, _ = w.Write([]byte(upstreamBody))
	}))
	defer upstream.Close()

	cfg := &Config{
		Request:  &Request{Method: "GET", URL: upstream.URL + "/", Body: ""},
		Response: &Response{},
	}
	h, _ := cfg.CreateRoute("GET", "/x", nil)
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/x", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	h(rr, req)

	// When no transform is set, the body should reach the caller intact.
	var got map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("upstream body not passed through: %s", rr.Body.String())
	}
	if _, ok := got["users"]; !ok {
		t.Fatalf("body missing 'users' key: %v", got)
	}
}
