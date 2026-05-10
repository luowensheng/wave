package plugin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wave/infra/plugins"
)

// fakeClient implements plugins.Client.
type fakeClient struct {
	got  *plugins.Request
	resp *plugins.Response
	err  error
}

func (f *fakeClient) Call(_ context.Context, req *plugins.Request) (*plugins.Response, error) {
	f.got = req
	return f.resp, f.err
}
func (f *fakeClient) Close() error { return nil }

func install(t *testing.T, name string, c plugins.Client) {
	t.Helper()
	plugins.SetDefault(buildRegWith(t, name, c))
}

func buildRegWith(t *testing.T, name string, c plugins.Client) *plugins.Registry {
	t.Helper()
	// We piggyback off NewRegistry then mutate via test-only helper below.
	reg, err := plugins.NewRegistry(nil)
	if err != nil {
		t.Fatal(err)
	}
	plugins.InjectForTest(reg, name, c)
	return reg
}

func TestPluginRouteRoundTrip(t *testing.T) {
	body, _ := json.Marshal(map[string]any{
		"success": true,
		"user":    map[string]any{"uid": "u1", "email": "e@x.io"},
	})
	fc := &fakeClient{resp: &plugins.Response{Status: 200, Body: body, Headers: map[string]string{"X-A": "1"}}}
	install(t, "fb", fc)

	cfg := &Config{
		Name:       "fb",
		TriggerKey: "verify",
		ResponseOutput: map[string]string{
			"success": "response.success",
			"user_id": "response.user.uid",
			"email":   "response.user.email",
		},
	}
	h, err := cfg.CreateRoute("POST", "/login", nil)
	if err != nil {
		t.Fatal(err)
	}

	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"token":"abc"}`))
	r.Header.Set("X-Real-IP", "10.0.0.1")
	r.Header.Set("Authorization", "Bearer xyz")
	h(rr, r)

	if rr.Code != 200 {
		t.Fatalf("status = %d", rr.Code)
	}
	if fc.got.TriggerKey != "verify" {
		t.Errorf("trigger_key = %q", fc.got.TriggerKey)
	}
	if fc.got.Metadata["remote_ip"] != "10.0.0.1" {
		t.Errorf("remote_ip = %q", fc.got.Metadata["remote_ip"])
	}
	out := rr.Body.String()
	if !strings.Contains(out, `"user_id":"u1"`) {
		t.Errorf("missing user_id: %s", out)
	}
	if !strings.Contains(out, `"email":"e@x.io"`) {
		t.Errorf("missing email: %s", out)
	}
	// Original wrapper response should not leak unmapped fields when
	// response_output is set (it's a whitelist).
	if strings.Contains(out, `"user"`) {
		t.Errorf("nested object leaked: %s", out)
	}
}

func TestPluginRouteUnknownPlugin(t *testing.T) {
	plugins.SetDefault(buildRegWith(t, "x", &fakeClient{resp: &plugins.Response{Status: 200}}))
	cfg := &Config{Name: "missing"}
	h, _ := cfg.CreateRoute("POST", "/x", nil)
	rr := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/x", strings.NewReader("{}"))
	h(rr, r)
	if rr.Code != http.StatusInternalServerError {
		t.Errorf("status = %d", rr.Code)
	}
}
