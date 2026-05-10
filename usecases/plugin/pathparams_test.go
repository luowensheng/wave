package plugin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wave/infra/plugins"
)

type capturingClient struct{ got *plugins.Request }

func (c *capturingClient) Call(_ context.Context, req *plugins.Request) (*plugins.Response, error) {
	c.got = req
	return &plugins.Response{Status: 200}, nil
}
func (c *capturingClient) Close() error { return nil }

func TestPluginRouteSurfacesPathParams(t *testing.T) {
	cap := &capturingClient{}
	reg, _ := plugins.NewRegistry(nil)
	plugins.InjectForTest(reg, "p", cap)
	plugins.SetDefault(reg)

	cfg := &Config{Name: "p", TriggerKey: "verify"}
	h, err := cfg.CreateRoute("POST", "/items/{id}/comments/{cid}", nil)
	if err != nil {
		t.Fatal(err)
	}

	// Real ServeMux populates r.PathValue + r.Pattern.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /items/{id}/comments/{cid}", h)

	w := httptest.NewRecorder()
	r := httptest.NewRequest("POST", "/items/abc/comments/42", strings.NewReader(`{"x":1}`))
	mux.ServeHTTP(w, r)

	if cap.got == nil {
		t.Fatal("plugin not called")
	}
	if cap.got.PathParams["id"] != "abc" || cap.got.PathParams["cid"] != "42" {
		t.Errorf("path params = %v", cap.got.PathParams)
	}
}

func TestExtractPathParamsNoPattern(t *testing.T) {
	r := httptest.NewRequest("GET", "/x", nil) // r.Pattern is empty
	if got := extractPathParams(r); got != nil {
		t.Errorf("expected nil, got %v", got)
	}
}
