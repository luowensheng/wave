package servers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"wave/orchestrator/features/auth"
	"wave/usecases/routes"
)

func TestOpenAPIDocumentsRoutes(t *testing.T) {
	mux := http.NewServeMux()
	s := &Server{
		Config: &Config{
			Auth: map[string]*auth.AuthConfig{
				"user_jwt": {Type: "jwt", TokenLocation: "header", HeaderScheme: "Bearer"},
			},
			Routes: []*Route{
				{Path: "/api/items", Method: "GET", Type: "api", Description: "List items", Auth: []string{"user_jwt"}},
				{Path: "/echo", Method: "POST", Type: "plugin",
					PluginConfig: &routes.PluginConfig{Name: "echo", TriggerKey: "hello"},
				},
				{Path: "/webhooks/test", Method: "POST", Type: "stream-publish",
					StreamPublishConfig: &routes.StreamPublishConfig{Connection: "payments", EventType: "payment", RouteID: "p"},
				},
			},
		},
		mux: mux,
	}
	s.registerOpenAPI()

	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/openapi.json", nil)
	mux.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Fatalf("status = %d", rr.Code)
	}

	var doc map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &doc); err != nil {
		t.Fatal(err)
	}
	if doc["openapi"] != "3.1.0" {
		t.Errorf("openapi = %v", doc["openapi"])
	}
	paths := doc["paths"].(map[string]any)

	// Check the configured routes are present at the right method.
	if _, ok := paths["/api/items"].(map[string]any)["get"]; !ok {
		t.Errorf("/api/items GET missing: %v", paths)
	}
	if _, ok := paths["/echo"].(map[string]any)["post"]; !ok {
		t.Errorf("/echo POST missing")
	}
	echo := paths["/echo"].(map[string]any)["post"].(map[string]any)
	if echo["x-wave-type"] != "plugin" {
		t.Errorf("missing x-wave-type")
	}
	plugin := echo["x-wave-plugin"].(map[string]any)
	if plugin["name"] != "echo" || plugin["trigger_key"] != "hello" {
		t.Errorf("plugin meta: %v", plugin)
	}

	wh := paths["/webhooks/test"].(map[string]any)["post"].(map[string]any)
	stream := wh["x-wave-stream"].(map[string]any)
	if stream["connection"] != "payments" || stream["event_type"] != "payment" {
		t.Errorf("stream meta: %v", stream)
	}

	// Operational endpoints get auto-added.
	for _, p := range []string{"/healthz", "/readyz", "/metrics", "/admin/", "/version"} {
		if _, ok := paths[p]; !ok {
			t.Errorf("missing operational path %q", p)
		}
	}

	// Security schemes emitted from auth config.
	comps := doc["components"].(map[string]any)
	schemes := comps["securitySchemes"].(map[string]any)
	uj := schemes["user_jwt"].(map[string]any)
	if uj["scheme"] != "bearer" {
		t.Errorf("user_jwt scheme = %v", uj["scheme"])
	}
}
