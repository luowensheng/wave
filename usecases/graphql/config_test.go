package graphql

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wave/infra/plugins"
)

type capClient struct {
	got  *plugins.Request
	resp *plugins.Response
}

func (c *capClient) Call(_ context.Context, r *plugins.Request) (*plugins.Response, error) {
	c.got = r
	return c.resp, nil
}
func (c *capClient) Close() error { return nil }

func TestGraphQLPostRoutesToPlugin(t *testing.T) {
	cap := &capClient{resp: &plugins.Response{
		Status: 200,
		Body:   []byte(`{"data":{"users":[{"id":1}]}}`),
	}}
	reg, _ := plugins.NewRegistry(nil)
	plugins.InjectForTest(reg, "resolver", cap)
	plugins.SetDefault(reg)

	cfg := &Config{Plugin: "resolver"}
	h, err := cfg.CreateRoute("POST", "/graphql", nil)
	if err != nil {
		t.Fatal(err)
	}
	body := strings.NewReader(`{"query":"{ users { id } }","operationName":"GetUsers"}`)
	r := httptest.NewRequest("POST", "/graphql", body)
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
	}
	if cap.got.TriggerKey != "GetUsers" {
		t.Errorf("trigger_key = %q", cap.got.TriggerKey)
	}
	if !strings.Contains(string(cap.got.Body), `"GetUsers"`) {
		t.Errorf("plugin didn't receive operationName: %s", cap.got.Body)
	}
	if !strings.Contains(w.Body.String(), `"users"`) {
		t.Errorf("response body unexpected: %q", w.Body.String())
	}
}

func TestGraphQLDefaultTriggerKey(t *testing.T) {
	cap := &capClient{resp: &plugins.Response{Status: 200}}
	reg, _ := plugins.NewRegistry(nil)
	plugins.InjectForTest(reg, "resolver", cap)
	plugins.SetDefault(reg)

	cfg := &Config{Plugin: "resolver"}
	h, _ := cfg.CreateRoute("POST", "/graphql", nil)
	r := httptest.NewRequest("POST", "/graphql",
		strings.NewReader(`{"query":"{ x }"}`))
	h(httptest.NewRecorder(), r)
	if cap.got.TriggerKey != "default" {
		t.Errorf("trigger_key = %q", cap.got.TriggerKey)
	}
}

func TestGraphQLMissingQueryRejected(t *testing.T) {
	cfg := &Config{Plugin: "x"}
	reg, _ := plugins.NewRegistry(nil)
	plugins.InjectForTest(reg, "x", &capClient{resp: &plugins.Response{Status: 200}})
	plugins.SetDefault(reg)

	h, _ := cfg.CreateRoute("POST", "/graphql", nil)
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest("POST", "/graphql", strings.NewReader(`{}`)))
	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), `"errors"`) {
		t.Errorf("body = %q", w.Body.String())
	}
}

func TestGraphQLIntrospectionRoutesToIntrospectPlugin(t *testing.T) {
	main := &capClient{resp: &plugins.Response{Status: 200}}
	intro := &capClient{resp: &plugins.Response{Status: 200}}
	reg, _ := plugins.NewRegistry(nil)
	plugins.InjectForTest(reg, "main", main)
	plugins.InjectForTest(reg, "intro", intro)
	plugins.SetDefault(reg)

	cfg := &Config{Plugin: "main", IntrospectPlugin: "intro"}
	h, _ := cfg.CreateRoute("POST", "/graphql", nil)
	body := `{"query":"query IntrospectionQuery { __schema { types { name } } }"}`
	h(httptest.NewRecorder(), httptest.NewRequest("POST", "/graphql", strings.NewReader(body)))
	if intro.got == nil {
		t.Fatal("introspect plugin not called")
	}
	if main.got != nil {
		t.Error("main plugin should not see introspection query")
	}
}

func TestGraphQLGet(t *testing.T) {
	cap := &capClient{resp: &plugins.Response{Status: 200}}
	reg, _ := plugins.NewRegistry(nil)
	plugins.InjectForTest(reg, "r", cap)
	plugins.SetDefault(reg)

	cfg := &Config{Plugin: "r"}
	h, _ := cfg.CreateRoute("GET", "/graphql", nil)
	r := httptest.NewRequest("GET", "/graphql?query={users{id}}&operationName=Q&variables={\"limit\":10}", nil)
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
	}
	if cap.got.TriggerKey != "Q" {
		t.Errorf("trigger = %q", cap.got.TriggerKey)
	}
	if !strings.Contains(string(cap.got.Body), `"limit":10`) {
		t.Errorf("variables not passed: %s", cap.got.Body)
	}
}
