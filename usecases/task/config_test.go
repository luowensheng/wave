package task

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"wave/infra/connections"
	"wave/infra/plugins"
	storageaccess "wave/usecases/storage_access"
	"wave/io/http/contentloader"
)

// fakeClient is a test double for plugins.Client.
type fakeClient struct {
	called     bool
	lastReq    *plugins.Request
	respBody   []byte
	respErr    error
}

func (f *fakeClient) Call(_ context.Context, req *plugins.Request) (*plugins.Response, error) {
	f.called = true
	f.lastReq = req
	if f.respErr != nil {
		return nil, f.respErr
	}
	return &plugins.Response{Status: 200, Body: f.respBody}, nil
}

func (f *fakeClient) Close() error { return nil }

// fakeStorage records Execute calls.
type fakeStorage struct {
	executed []string
	dataLoaders []*contentloader.DataLoader
	result   any
	err      error
}

func (f *fakeStorage) Execute(cmd string, dl *contentloader.DataLoader) (any, error) {
	f.executed = append(f.executed, cmd)
	f.dataLoaders = append(f.dataLoaders, dl)
	return f.result, f.err
}

func setupPluginRegistry(t *testing.T, name string, client plugins.Client) *plugins.Registry {
	t.Helper()
	reg := &plugins.Registry{}
	plugins.InjectForTest(reg, name, client)
	plugins.SetDefault(reg)
	t.Cleanup(func() { plugins.SetDefault(nil) })
	return reg
}

func setupConnectionRegistry(t *testing.T, name string) (*connections.Registry, *connections.Broker) {
	t.Helper()
	cfg := map[string]*connections.ConnectionConfig{
		name: {
			Type:          "sse",
			SubscribePath: "/events/" + name,
			BufferSize:    16,
		},
	}
	reg, err := connections.NewRegistry(cfg)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	connections.SetDefault(reg)
	t.Cleanup(func() { connections.SetDefault(nil) })
	broker, _ := reg.Get(name)
	return reg, broker
}

// TestCreateRoute_MissingPlugin verifies CreateRoute fails without a plugin name.
func TestCreateRoute_MissingPlugin(t *testing.T) {
	c := &Config{Connection: "results"}
	_, err := c.CreateRoute("POST", "/test", nil)
	if err == nil {
		t.Fatal("expected error for missing plugin, got nil")
	}
	if !strings.Contains(err.Error(), "plugin is required") {
		t.Errorf("expected 'plugin is required' error, got: %v", err)
	}
}

// TestCreateRoute_MissingConnection verifies CreateRoute fails without a connection name.
func TestCreateRoute_MissingConnection(t *testing.T) {
	c := &Config{Plugin: "myplugin"}
	_, err := c.CreateRoute("POST", "/test", nil)
	if err == nil {
		t.Fatal("expected error for missing connection, got nil")
	}
	if !strings.Contains(err.Error(), "connection is required") {
		t.Errorf("expected 'connection is required' error, got: %v", err)
	}
}

// TestCreateRoute_Returns202 verifies the handler returns 202 + task_id.
func TestCreateRoute_Returns202(t *testing.T) {
	fc := &fakeClient{respBody: []byte(`{"result":"ok"}`)}
	setupPluginRegistry(t, "myplugin", fc)
	setupConnectionRegistry(t, "results")

	c := &Config{Plugin: "myplugin", Connection: "results"}
	handler, err := c.CreateRoute("POST", "/test", nil)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	req := httptest.NewRequest("POST", "/test", strings.NewReader(`{"prompt":"hi"}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)

	if rr.Code != http.StatusAccepted {
		t.Errorf("expected 202, got %d", rr.Code)
	}

	var resp map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &resp); err != nil {
		t.Fatalf("parse response body: %v", err)
	}
	if resp["task_id"] == "" {
		t.Error("expected non-empty task_id")
	}
}

// TestCreateRoute_PluginCalledWithBody verifies the plugin receives the request body.
func TestCreateRoute_PluginCalledWithBody(t *testing.T) {
	fc := &fakeClient{respBody: []byte(`{"result":"ok"}`)}
	setupPluginRegistry(t, "myplugin", fc)
	setupConnectionRegistry(t, "results")

	c := &Config{Plugin: "myplugin", Connection: "results", TriggerKey: "process"}
	handler, err := c.CreateRoute("POST", "/test", nil)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	body := `{"prompt":"hello world"}`
	req := httptest.NewRequest("POST", "/test", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	handler(rr, req)

	// Wait for goroutine to call the plugin.
	deadline := time.Now().Add(2 * time.Second)
	for !fc.called && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if !fc.called {
		t.Fatal("plugin was not called")
	}
	if fc.lastReq.TriggerKey != "process" {
		t.Errorf("expected trigger_key 'process', got %q", fc.lastReq.TriggerKey)
	}
	if string(fc.lastReq.Body) != body {
		t.Errorf("expected body %q, got %q", body, string(fc.lastReq.Body))
	}
}

// TestCreateRoute_SSEEventPublished verifies the SSE broker receives the event.
func TestCreateRoute_SSEEventPublished(t *testing.T) {
	fc := &fakeClient{respBody: []byte(`{"result":"hello"}`)}
	setupPluginRegistry(t, "myplugin", fc)
	_, broker := setupConnectionRegistry(t, "results")

	// Subscribe before triggering.
	ch, cancel, ok := broker.Subscribe("test-sub")
	if !ok {
		t.Fatal("subscribe failed")
	}
	defer cancel()

	c := &Config{Plugin: "myplugin", Connection: "results", EventType: "result"}
	handler, err := c.CreateRoute("POST", "/test", nil)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	req := httptest.NewRequest("POST", "/test", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	// Wait for the SSE event.
	select {
	case event := <-ch:
		s := string(event)
		if !strings.Contains(s, "event: result") {
			t.Errorf("expected 'event: result' in SSE event, got: %q", s)
		}
		if !strings.Contains(s, `{"result":"hello"}`) {
			t.Errorf("expected payload in SSE event, got: %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for SSE event")
	}
}

// TestCreateRoute_StoreWrites verifies storage.Execute is called when store is configured.
func TestCreateRoute_StoreWrites(t *testing.T) {
	fc := &fakeClient{respBody: []byte(`{"content":"some text"}`)}
	setupPluginRegistry(t, "myplugin", fc)
	setupConnectionRegistry(t, "results")

	fs := &fakeStorage{}
	oldGetStorageFn := GetStorageFn
	GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
		if name == "mydb" {
			return fs, true
		}
		return nil, false
	}
	defer func() { GetStorageFn = oldGetStorageFn }()

	c := &Config{
		Plugin:     "myplugin",
		Connection: "results",
		Store: &StoreConfig{
			Source:  "mydb",
			Execute: "INSERT INTO results (content) VALUES ({{content}})",
			Inputs:  map[string]string{"content": "event.content"},
		},
	}
	handler, err := c.CreateRoute("POST", "/test", nil)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	req := httptest.NewRequest("POST", "/test", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	// Wait for goroutine to finish.
	deadline := time.Now().Add(2 * time.Second)
	for len(fs.executed) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if len(fs.executed) == 0 {
		t.Fatal("storage.Execute was not called")
	}
	if fs.executed[0] != "INSERT INTO results (content) VALUES ({{content}})" {
		t.Errorf("unexpected execute: %q", fs.executed[0])
	}
}

// TestCreateRoute_StoreEmptyFromPath verifies CreateRoute returns an error at boot
// when a store input has an empty from-path.
func TestCreateRoute_StoreEmptyFromPath(t *testing.T) {
	oldGetStorageFn := GetStorageFn
	GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
		if name == "mydb" {
			return &fakeStorage{}, true
		}
		return nil, false
	}
	defer func() { GetStorageFn = oldGetStorageFn }()

	c := &Config{
		Plugin:     "myplugin",
		Connection: "results",
		Store: &StoreConfig{
			Source:  "mydb",
			Execute: "INSERT INTO results (content) VALUES ({{content}})",
			Inputs:  map[string]string{"content": ""},
		},
	}
	_, err := c.CreateRoute("POST", "/test", nil)
	if err == nil {
		t.Fatal("expected error for empty from-path, got nil")
	}
	if !strings.Contains(err.Error(), "from-path is empty") {
		t.Errorf("expected 'from-path is empty' in error, got: %v", err)
	}
}

// TestCreateRoute_EventNamespace verifies that the event.* namespace works end-to-end:
// the plugin response is stored under "event" and dot-paths like "event.content"
// correctly resolve when storage.Execute is called.
func TestCreateRoute_EventNamespace(t *testing.T) {
	fc := &fakeClient{respBody: []byte(`{"content":"hello","score":42}`)}
	setupPluginRegistry(t, "myplugin", fc)
	setupConnectionRegistry(t, "results")

	fs := &fakeStorage{}
	oldGetStorageFn := GetStorageFn
	GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
		if name == "mydb" {
			return fs, true
		}
		return nil, false
	}
	defer func() { GetStorageFn = oldGetStorageFn }()

	const sqlTemplate = "INSERT INTO results (content, score) VALUES ({{content}}, {{score}})"
	c := &Config{
		Plugin:     "myplugin",
		Connection: "results",
		Store: &StoreConfig{
			Source:  "mydb",
			Execute: sqlTemplate,
			Inputs: map[string]string{
				"content": "event.content",
				"score":   "event.score",
			},
		},
	}
	handler, err := c.CreateRoute("POST", "/test", nil)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	req := httptest.NewRequest("POST", "/test", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	// Wait for the goroutine to finish.
	deadline := time.Now().Add(2 * time.Second)
	for len(fs.executed) == 0 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}

	if len(fs.executed) == 0 {
		t.Fatal("storage.Execute was not called")
	}
	if fs.executed[0] != sqlTemplate {
		t.Errorf("unexpected execute SQL: got %q, want %q", fs.executed[0], sqlTemplate)
	}
}

// TestCreateRoute_StreamingMode verifies ndjson lines are emitted as separate events.
func TestCreateRoute_StreamingMode(t *testing.T) {
	ndjson := `{"line":1}` + "\n" + `{"line":2}` + "\n"
	fc := &fakeClient{respBody: []byte(ndjson)}
	setupPluginRegistry(t, "myplugin", fc)
	_, broker := setupConnectionRegistry(t, "results")

	ch, cancel, ok := broker.Subscribe("test-sub")
	if !ok {
		t.Fatal("subscribe failed")
	}
	defer cancel()

	c := &Config{Plugin: "myplugin", Connection: "results", Streaming: true}
	handler, err := c.CreateRoute("POST", "/test", nil)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}

	req := httptest.NewRequest("POST", "/test", nil)
	rr := httptest.NewRecorder()
	handler(rr, req)

	received := 0
	timeout := time.After(2 * time.Second)
	for received < 2 {
		select {
		case event := <-ch:
			s := string(event)
			if strings.Contains(s, `{"line":`) {
				received++
			}
		case <-timeout:
			t.Fatalf("timeout: only received %d/2 events", received)
		}
	}
	if received != 2 {
		t.Errorf("expected 2 SSE events, got %d", received)
	}
}
