package servers

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wave/infra/connections"
	"wave/infra/plugins"
	"wave/usecases/routes"
)

func TestAdminDashboardRenders(t *testing.T) {
	creg, _ := connections.NewRegistry(map[string]*connections.ConnectionConfig{
		"payments": {Type: "sse", SubscribePath: "/events/payments"},
	})
	connections.SetDefault(creg)

	preg, _ := plugins.NewRegistry(map[string]*plugins.PluginConfig{
		"echo": {Transport: "process", Command: "./echo"},
	})
	plugins.SetDefault(preg)

	mux := http.NewServeMux()
	s := &Server{
		Config: &Config{
			Plugins: map[string]*plugins.PluginConfig{"echo": {Transport: "process", Command: "./echo"}},
			Routes: []*Route{
				{Path: "/echo", Method: "POST", Type: "plugin",
					PluginConfig: &routes.PluginConfig{Name: "echo"},
					Description:  "echo plugin"},
				{Path: "/webhooks/test", Method: "POST", Type: "stream-publish",
					StreamPublishConfig: &routes.StreamPublishConfig{Connection: "payments"}},
			},
		},
		mux: mux,
	}
	s.registerAdminDashboard()

	rr := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/admin/", nil)
	mux.ServeHTTP(rr, r)
	if rr.Code != 200 {
		t.Fatalf("status = %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		"wave",
		"/echo",
		"plugin",
		"stream-publish",
		"payments",
		"echo plugin",
		"/events/payments",
		"wave_plugin_echo_calls_total",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q", want)
		}
	}
}
