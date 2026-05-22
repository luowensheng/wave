package fetch

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wave/infra/inputs"
	"wave/usecases/schedule"
)

// TestCreateRoute_MissingAction verifies that CreateRoute fails when Action is nil.
func TestCreateRoute_MissingAction(t *testing.T) {
	c := &Config{
		OutputTemplate:      `{"ok":true}`,
		ResponseContentType: "application/json",
	}
	_, err := c.CreateRoute("GET", "/test", nil)
	if err == nil {
		t.Fatal("expected error for missing action, got nil")
	}
	if !strings.Contains(err.Error(), "action is required") {
		t.Errorf("expected 'action is required' error, got: %v", err)
	}
}

// TestCreateRoute_MissingOutputTemplate verifies that CreateRoute fails when
// OutputTemplate is empty.
func TestCreateRoute_MissingOutputTemplate(t *testing.T) {
	c := &Config{
		Action:              &schedule.Action{Type: "api", URL: "http://example.com"},
		ResponseContentType: "application/json",
	}
	_, err := c.CreateRoute("GET", "/test", nil)
	if err == nil {
		t.Fatal("expected error for missing output_template, got nil")
	}
	if !strings.Contains(err.Error(), "output_template is required") {
		t.Errorf("expected 'output_template is required' error, got: %v", err)
	}
}

// TestCreateRoute_MissingResponseContentType verifies that CreateRoute fails
// when ResponseContentType is empty.
func TestCreateRoute_MissingResponseContentType(t *testing.T) {
	c := &Config{
		Action:         &schedule.Action{Type: "api", URL: "http://example.com"},
		OutputTemplate: `{"ok":true}`,
	}
	_, err := c.CreateRoute("GET", "/test", nil)
	if err == nil {
		t.Fatal("expected error for missing response_content_type, got nil")
	}
	if !strings.Contains(err.Error(), "response_content_type is required") {
		t.Errorf("expected 'response_content_type is required' error, got: %v", err)
	}
}

// TestCreateRoute_SinkEmptyFromPath verifies that CreateRoute fails at boot when
// a sink declares an input with an empty from-path.
func TestCreateRoute_SinkEmptyFromPath(t *testing.T) {
	c := &Config{
		Action: &schedule.Action{
			Type:   "api",
			URL:    "http://example.com",
			Output: "weather",
		},
		Then: []*schedule.Sink{
			{
				Type:    "storage",
				Source:  "mydb",
				Execute: "INSERT INTO t (v) VALUES ({{v}})",
				Inputs:  map[string]string{"v": ""},
			},
		},
		OutputTemplate:      `{{toJSON .weather}}`,
		ResponseContentType: "application/json",
	}
	_, err := c.CreateRoute("GET", "/test", nil)
	if err == nil {
		t.Fatal("expected error for empty sink from-path, got nil")
	}
	if !strings.Contains(err.Error(), "from-path is empty") {
		t.Errorf("expected 'from-path is empty' in error, got: %v", err)
	}
}

// TestCreateRoute_Handler_200 verifies a full happy-path fetch route:
// the action calls a real test HTTP server, stores the result under
// action.Output, and the output_template renders it correctly.
func TestCreateRoute_Handler_200(t *testing.T) {
	// Spin up a test server returning a JSON response.
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"temp":22.5}`))
	}))
	defer ts.Close()

	// Reset schedule dependencies to nil (no sinks requiring storage/connections/plugins).
	oldGetStorageFn := schedule.GetStorageFn
	oldGetConnectionFn := schedule.GetConnectionFn
	oldGetPluginFn := schedule.GetPluginFn
	schedule.GetStorageFn = nil
	schedule.GetConnectionFn = nil
	schedule.GetPluginFn = nil
	defer func() {
		schedule.GetStorageFn = oldGetStorageFn
		schedule.GetConnectionFn = oldGetConnectionFn
		schedule.GetPluginFn = oldGetPluginFn
	}()

	c := &Config{
		Action: &schedule.Action{
			Type:   "api",
			URL:    ts.URL,
			Method: "GET",
			Output: "weather",
		},
		OutputTemplate:      `{"t":{{.weather.json.temp}}}`,
		ResponseContentType: "application/json",
	}

	handler, err := c.CreateRoute("GET", "/weather", nil)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	req := httptest.NewRequest("GET", "/weather", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}
	ct := rr.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Errorf("expected Content-Type application/json, got %q", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, `"t":22.5`) {
		t.Errorf("expected body to contain `\"t\":22.5`, got: %s", body)
	}
}

// TestCreateRoute_Handler_InputsNamespace verifies that accum["inputs"] is seeded
// from declared route inputs (via inputs.WithValues on the context), and that the
// fetch handler makes them available for action vars.
func TestCreateRoute_Handler_InputsNamespace(t *testing.T) {
	// Test HTTP server that captures the city query param sent to it.
	var capturedURL string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer ts.Close()

	// Reset schedule dependencies.
	oldGetStorageFn := schedule.GetStorageFn
	oldGetConnectionFn := schedule.GetConnectionFn
	oldGetPluginFn := schedule.GetPluginFn
	schedule.GetStorageFn = nil
	schedule.GetConnectionFn = nil
	schedule.GetPluginFn = nil
	defer func() {
		schedule.GetStorageFn = oldGetStorageFn
		schedule.GetConnectionFn = oldGetConnectionFn
		schedule.GetPluginFn = oldGetPluginFn
	}()

	// Build a URL template that uses a var interpolated from accum["inputs"].
	// The httpclient will substitute vars["city"] into the URL at call time.
	c := &Config{
		Action: &schedule.Action{
			Type:   "api",
			URL:    ts.URL + "?city={{city}}",
			Method: "GET",
			Output: "result",
			Vars:   map[string]string{"city": "inputs.city"},
		},
		OutputTemplate:      `{{toJSON .result}}`,
		ResponseContentType: "application/json",
	}

	handler, err := c.CreateRoute("GET", "/fetch-city", nil)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	// Inject declared input values onto the request context (as the inputs
	// middleware would do at runtime).
	req := httptest.NewRequest("GET", "/fetch-city", nil)
	req = req.WithContext(inputs.WithValues(req.Context(), map[string]any{"city": "London"}))

	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("expected 200, got %d — body: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(capturedURL, "London") {
		t.Errorf("expected captured URL to contain 'London', got %q", capturedURL)
	}
}

