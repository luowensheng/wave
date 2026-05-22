package schedule

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"wave/infra/connections"
	"wave/infra/httpclient"
	"wave/infra/plugins"
	"wave/io/http/contentloader"
	storageaccess "wave/usecases/storage_access"
)

// ── stubs ────────────────────────────────────────────────────────────────────

// stubStorage implements storageaccess.StorageRef.
type stubStorage struct {
	result      any
	err         error
	capturedSQL string
	capturedDL  *contentloader.DataLoader
}

func (s *stubStorage) Execute(command string, data *contentloader.DataLoader) (any, error) {
	s.capturedSQL = command
	s.capturedDL = data
	return s.result, s.err
}

// stubPluginClient implements plugins.Client.
type stubPluginClient struct {
	responseBody []byte
	capturedReq  *plugins.Request
}

func (s *stubPluginClient) Call(ctx context.Context, req *plugins.Request) (*plugins.Response, error) {
	s.capturedReq = req
	return &plugins.Response{Status: 200, Body: s.responseBody}, nil
}

func (s *stubPluginClient) Close() error { return nil }

// resetFns restores injected function pointers to nil.
func resetFns(t *testing.T) {
	t.Cleanup(func() {
		GetStorageFn = nil
		GetConnectionFn = nil
		GetPluginFn = nil
		httpclient.SetDefault(nil)
	})
}

// ── ExecuteAction tests ───────────────────────────────────────────────────────

func TestExecuteAction_APIInline(t *testing.T) {
	resetFns(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"price":9.99}`))
	}))
	defer srv.Close()

	action := &Action{
		Type:   "api",
		URL:    srv.URL,
		Method: "GET",
	}
	result, err := ExecuteAction(context.Background(), action, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Parsed JSON lives under "json" — not merged at top level.
	j, ok := result["json"].(map[string]any)
	if !ok {
		t.Fatalf("expected json to be map, got %T", result["json"])
	}
	if j["price"] != 9.99 {
		t.Errorf("expected json.price=9.99, got %v", j["price"])
	}
	if result["status"] != 200 {
		t.Errorf("expected status=200, got %v", result["status"])
	}
	if _, leaked := result["price"]; leaked {
		t.Error("price leaked to top level — JSON must only live under .json")
	}
}

func TestExecuteAction_APIWithRef(t *testing.T) {
	resetFns(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"price":9.99}`))
	}))
	defer srv.Close()

	reg, err := httpclient.NewRegistry(map[string]*httpclient.RequestDef{
		"prices": {URL: srv.URL, Method: "GET"},
	})
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	httpclient.SetDefault(reg)

	action := &Action{
		Type: "api",
		Ref:  "prices",
	}
	result, err := ExecuteAction(context.Background(), action, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	j, ok := result["json"].(map[string]any)
	if !ok {
		t.Fatalf("expected json key to be a map, got %T", result["json"])
	}
	if j["price"] != 9.99 {
		t.Errorf("expected json.price=9.99, got %v", j["price"])
	}
}

func TestExecuteAction_APIWithVars(t *testing.T) {
	resetFns(t)

	var receivedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	// URL contains a {{city}} placeholder substituted by vars.
	action := &Action{
		Type:   "api",
		URL:    srv.URL + "/weather/{{city}}",
		Method: "GET",
		Vars:   map[string]string{"city": "inputs.city"},
	}
	accum := map[string]any{
		"inputs": map[string]any{"city": "London"},
	}
	_, err := ExecuteAction(context.Background(), action, accum)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(receivedURL, "London") {
		t.Errorf("expected URL to contain 'London', got %q", receivedURL)
	}
}

func TestExecuteAction_Storage(t *testing.T) {
	resetFns(t)

	fakeResult := struct{ Data any }{Data: map[string]any{"rows": float64(5)}}
	stub := &stubStorage{result: fakeResult}
	GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
		return stub, name == "db"
	}

	action := &Action{
		Type:    "storage",
		Source:  "db",
		Execute: "SELECT COUNT(*) AS rows FROM items",
	}
	result, err := ExecuteAction(context.Background(), action, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, ok := result["data"]; !ok {
		t.Errorf("expected 'data' key in result, got %v", result)
	}
}

func TestExecuteAction_Plugin(t *testing.T) {
	resetFns(t)

	stub := &stubPluginClient{responseBody: []byte(`{"ok":true}`)}
	GetPluginFn = func(name string) (plugins.Client, bool) {
		return stub, name == "worker"
	}

	action := &Action{
		Type:       "plugin",
		Plugin:     "worker",
		TriggerKey: "run",
	}
	result, err := ExecuteAction(context.Background(), action, map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Plugin result mirrors api shape: parsed body under "json".
	j, ok := result["json"].(map[string]any)
	if !ok {
		t.Fatalf("expected json to be map, got %T", result["json"])
	}
	if j["ok"] != true {
		t.Errorf("expected json.ok=true, got %v", j["ok"])
	}
}

// ── ApplySinks tests ──────────────────────────────────────────────────────────

func TestApplySinks_Storage(t *testing.T) {
	resetFns(t)

	stub := &stubStorage{result: nil}
	GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
		return stub, name == "db"
	}

	accum := map[string]any{
		"result": map[string]any{"price": 9.99},
	}
	sinks := []*Sink{
		{
			Type:    "storage",
			Source:  "db",
			Execute: "INSERT INTO t (p) VALUES ({{p}})",
			Inputs:  map[string]string{"p": "result.price"},
		},
	}
	if err := ApplySinks(context.Background(), sinks, accum); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.capturedSQL != "INSERT INTO t (p) VALUES ({{p}})" {
		t.Errorf("unexpected SQL: %q", stub.capturedSQL)
	}
}

func TestApplySinks_Publish(t *testing.T) {
	resetFns(t)

	connCfg := map[string]*connections.ConnectionConfig{
		"events": {Type: "sse", SubscribePath: "/events", BufferSize: 8},
	}
	reg, err := connections.NewRegistry(connCfg)
	if err != nil {
		t.Fatalf("build connections registry: %v", err)
	}

	broker, ok := reg.Get("events")
	if !ok {
		t.Fatal("broker 'events' not found")
	}
	ch, unsub, _ := broker.Subscribe("test-client")
	defer unsub()

	GetConnectionFn = func(name string) (*connections.Broker, bool) {
		return reg.Get(name)
	}

	accum := map[string]any{"value": 42}
	sinks := []*Sink{
		{Type: "publish", Connection: "events", EventType: "update"},
	}
	if err := ApplySinks(context.Background(), sinks, accum); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	select {
	case msg := <-ch:
		s := string(msg)
		if !strings.Contains(s, "event: update") {
			t.Errorf("expected SSE event line, got: %q", s)
		}
		if !strings.Contains(s, `"value"`) {
			t.Errorf("expected accum in SSE data, got: %q", s)
		}
	default:
		t.Fatal("no SSE event received on subscription channel")
	}
}

func TestApplySinks_Plugin(t *testing.T) {
	resetFns(t)

	stub := &stubPluginClient{responseBody: []byte(`{}`)}
	GetPluginFn = func(name string) (plugins.Client, bool) {
		return stub, name == "notifier"
	}

	accum := map[string]any{"result": map[string]any{"price": 9.99}}
	sinks := []*Sink{
		{Type: "plugin", Plugin: "notifier", TriggerKey: "notify"},
	}
	if err := ApplySinks(context.Background(), sinks, accum); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if stub.capturedReq == nil {
		t.Fatal("expected plugin Call to be invoked")
	}
	if stub.capturedReq.TriggerKey != "notify" {
		t.Errorf("expected trigger_key 'notify', got %q", stub.capturedReq.TriggerKey)
	}

	// Verify the body is the JSON-marshalled accum.
	var body map[string]any
	if err := json.Unmarshal(stub.capturedReq.Body, &body); err != nil {
		t.Fatalf("plugin body is not valid JSON: %v", err)
	}
	if _, ok := body["result"]; !ok {
		t.Errorf("expected 'result' key in plugin body, got %v", body)
	}
}

// ── api & for_each sink tests ────────────────────────────────────────────────

func TestApplySinks_API(t *testing.T) {
	resetFns(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","id":42}`))
	}))
	defer srv.Close()

	accum := map[string]any{}
	sinks := []*Sink{
		{Type: "api", URL: srv.URL, Output: "resp"},
	}
	if err := ApplySinks(context.Background(), sinks, accum); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, ok := accum["resp"].(map[string]any)
	if !ok {
		t.Fatalf("expected resp to be map, got %T", accum["resp"])
	}
	j, ok := resp["json"].(map[string]any)
	if !ok {
		t.Fatalf("expected resp.json to be map, got %T", resp["json"])
	}
	if j["id"] != float64(42) {
		t.Errorf("expected resp.json.id=42, got %v", j["id"])
	}
	if resp["status"] != 200 {
		t.Errorf("expected resp.status=200, got %v", resp["status"])
	}
}

func TestApplySinks_APIWithRef(t *testing.T) {
	resetFns(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok","id":42}`))
	}))
	defer srv.Close()

	reg, err := httpclient.NewRegistry(map[string]*httpclient.RequestDef{
		"r": {URL: srv.URL, Method: "GET"},
	})
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	httpclient.SetDefault(reg)

	accum := map[string]any{}
	sinks := []*Sink{
		{Type: "api", Ref: "r", Output: "resp"},
	}
	if err := ApplySinks(context.Background(), sinks, accum); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	resp, ok := accum["resp"].(map[string]any)
	if !ok {
		t.Fatalf("expected resp to be map, got %T", accum["resp"])
	}
	j, ok := resp["json"].(map[string]any)
	if !ok {
		t.Fatalf("expected resp.json to be map, got %T", resp["json"])
	}
	if j["id"] != float64(42) {
		t.Errorf("expected resp.json.id=42, got %v", j["id"])
	}
	if resp["status"] != 200 {
		t.Errorf("expected resp.status=200, got %v", resp["status"])
	}
}

func TestApplySinks_APIWithVars(t *testing.T) {
	resetFns(t)

	var receivedURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedURL = r.URL.String()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	reg, err := httpclient.NewRegistry(map[string]*httpclient.RequestDef{
		"r": {URL: srv.URL + "/users/{{name}}", Method: "GET"},
	})
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	httpclient.SetDefault(reg)

	accum := map[string]any{
		"inputs": map[string]any{"name": "Alice"},
	}
	sinks := []*Sink{
		{Type: "api", Ref: "r", Vars: map[string]string{"name": "inputs.name"}, Output: "resp"},
	}
	if err := ApplySinks(context.Background(), sinks, accum); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(receivedURL, "Alice") {
		t.Errorf("expected URL to contain 'Alice', got %q", receivedURL)
	}
}

func TestApplySinks_ForEach(t *testing.T) {
	resetFns(t)

	stub := &stubStorage{result: nil}
	GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
		return stub, name == "db"
	}

	accum := map[string]any{
		"peek": map[string]any{
			"data": []any{
				map[string]any{"id": 1, "label": "a"},
				map[string]any{"id": 2, "label": "b"},
			},
		},
	}
	sinks := []*Sink{
		{
			Type: "for_each",
			In:   "peek.data",
			As:   "item",
			Do: []*Sink{
				{
					Type:    "storage",
					Source:  "db",
					Inputs:  map[string]string{"id": "item.id", "label": "item.label"},
					Execute: "INSERT INTO t(id, label) VALUES({{id}}, {{label}})",
				},
			},
		},
	}

	// Wrap the stub so we can count calls and capture all SQL.
	calls := 0
	wrapper := &countingStorage{inner: stub, onCall: func() { calls++ }}
	GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
		return wrapper, name == "db"
	}

	if err := ApplySinks(context.Background(), sinks, accum); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 2 {
		t.Errorf("expected 2 storage calls, got %d", calls)
	}
	if wrapper.lastSQL != "INSERT INTO t(id, label) VALUES({{id}}, {{label}})" {
		t.Errorf("unexpected SQL: %q", wrapper.lastSQL)
	}
}

func TestApplySinks_ForEachAPI(t *testing.T) {
	resetFns(t)

	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`))
	}))
	defer srv.Close()

	reg, err := httpclient.NewRegistry(map[string]*httpclient.RequestDef{
		"r": {URL: srv.URL + "/u/{{url}}", Method: "GET"},
	})
	if err != nil {
		t.Fatalf("build registry: %v", err)
	}
	httpclient.SetDefault(reg)

	accum := map[string]any{
		"items": []any{
			map[string]any{"url": "a"},
			map[string]any{"url": "b"},
			map[string]any{"url": "c"},
		},
	}
	sinks := []*Sink{
		{
			Type: "for_each",
			In:   "items",
			As:   "item",
			Do: []*Sink{
				{Type: "api", Ref: "r", Vars: map[string]string{"url": "item.url"}, Output: "resp"},
			},
		},
	}
	if err := ApplySinks(context.Background(), sinks, accum); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 server calls, got %d", calls)
	}
}

func TestApplySinks_ForEachEmpty(t *testing.T) {
	resetFns(t)

	stub := &stubStorage{result: nil}
	called := 0
	wrapper := &countingStorage{inner: stub, onCall: func() { called++ }}
	GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
		return wrapper, name == "db"
	}

	accum := map[string]any{
		"items": []any{},
	}
	sinks := []*Sink{
		{
			Type: "for_each",
			In:   "items",
			As:   "item",
			Do: []*Sink{
				{Type: "storage", Source: "db", Execute: "INSERT INTO t VALUES (1)"},
			},
		},
	}
	if err := ApplySinks(context.Background(), sinks, accum); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if called != 0 {
		t.Errorf("expected 0 inner calls, got %d", called)
	}
}

func TestApplySinks_ForEachNotArray(t *testing.T) {
	resetFns(t)

	accum := map[string]any{
		"items": "not an array",
	}
	sinks := []*Sink{
		{
			Type: "for_each",
			In:   "items",
			As:   "item",
			Do: []*Sink{
				{Type: "publish", Connection: "x"},
			},
		},
	}
	err := ApplySinks(context.Background(), sinks, accum)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "expected array") {
		t.Errorf("expected 'expected array' in error, got: %v", err)
	}
}

// countingStorage wraps a stubStorage and counts calls.
type countingStorage struct {
	inner   *stubStorage
	onCall  func()
	lastSQL string
}

func (c *countingStorage) Execute(command string, data *contentloader.DataLoader) (any, error) {
	if c.onCall != nil {
		c.onCall()
	}
	c.lastSQL = command
	return c.inner.Execute(command, data)
}

// recordingStorage records every (sql, dl) call so per-iteration writes
// can be inspected.
type recordingStorage struct {
	calls []struct {
		sql string
		dl  *contentloader.DataLoader
	}
	result any
}

func (r *recordingStorage) Execute(command string, data *contentloader.DataLoader) (any, error) {
	r.calls = append(r.calls, struct {
		sql string
		dl  *contentloader.DataLoader
	}{command, data})
	return r.result, nil
}

// ── End-to-end integration tests ──────────────────────────────────────────────

// TestWorkerLoop_EndToEnd simulates the full queue-worker pattern:
// action peeks N rows from "storage", for_each calls an HTTP API per row,
// stores the response, and acks the row. Mirrors the queue-worker-demo
// server.yaml shape — proves the chain works as a real worker would.
func TestWorkerLoop_EndToEnd(t *testing.T) {
	resetFns(t)

	// Upstream HTTP server — returns a per-URL response.
	requestsSeen := []string{}
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsSeen = append(requestsSeen, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"fetched":"` + r.URL.Path + `","ok":true}`))
	}))
	defer upstream.Close()

	// Register the named request def used by the api sink.
	reg, err := httpclient.NewRegistry(map[string]*httpclient.RequestDef{
		"fetch_url": {URL: upstream.URL + "/{{path}}", Method: "GET"},
	})
	if err != nil {
		t.Fatalf("registry: %v", err)
	}
	httpclient.SetDefault(reg)

	// Fake storage — peek returns 3 rows, every other call records the write.
	peekResult := struct {
		Data []map[string]any
	}{
		Data: []map[string]any{
			{"id": 1, "path": "alpha"},
			{"id": 2, "path": "beta"},
			{"id": 3, "path": "gamma"},
		},
	}
	peekStorage := &stubStorage{result: peekResult}
	writeStorage := &recordingStorage{}
	GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
		switch name {
		case "peek":
			return peekStorage, true
		case "writes":
			return writeStorage, true
		}
		return nil, false
	}

	// Phase 1: action peeks the queue.
	action := &Action{
		Type:    "storage",
		Source:  "peek",
		Execute: "SELECT id, path FROM tasks WHERE status='pending' LIMIT 5",
		Output:  "peek",
	}
	accum := map[string]any{}
	result, err := ExecuteAction(context.Background(), action, accum)
	if err != nil {
		t.Fatalf("ExecuteAction: %v", err)
	}
	accum[action.Output] = result

	// Phase 2: for_each runs api + storage + storage per row.
	sinks := []*Sink{
		{
			Type: "for_each",
			In:   "peek.data",
			As:   "task",
			Do: []*Sink{
				{
					Type:   "api",
					Ref:    "fetch_url",
					Vars:   map[string]string{"path": "task.path"},
					Output: "resp",
				},
				{
					Type:   "storage",
					Source: "writes",
					Inputs: map[string]string{
						"task_id": "task.id",
						"body":    "resp.text", // raw response body string
					},
					Execute: "INSERT INTO results(task_id, body) VALUES({{task_id}}, {{body}})",
				},
				{
					Type:   "storage",
					Source: "writes",
					Inputs: map[string]string{"id": "task.id"},
					Execute: "UPDATE tasks SET status='done' WHERE id={{id}}",
				},
			},
		},
	}
	if err := ApplySinks(context.Background(), sinks, accum); err != nil {
		t.Fatalf("ApplySinks: %v", err)
	}

	// Verify: 3 HTTP calls (one per row) at correct paths.
	if len(requestsSeen) != 3 {
		t.Fatalf("expected 3 upstream calls, got %d: %v", len(requestsSeen), requestsSeen)
	}
	expected := []string{"/alpha", "/beta", "/gamma"}
	for i, want := range expected {
		if requestsSeen[i] != want {
			t.Errorf("call %d: expected %q, got %q", i, want, requestsSeen[i])
		}
	}

	// Verify: 6 storage writes (3 inserts + 3 updates, alternating).
	if len(writeStorage.calls) != 6 {
		t.Fatalf("expected 6 storage writes, got %d", len(writeStorage.calls))
	}
	for i := 0; i < 3; i++ {
		insertSQL := writeStorage.calls[i*2].sql
		updateSQL := writeStorage.calls[i*2+1].sql
		if !strings.Contains(insertSQL, "INSERT INTO results") {
			t.Errorf("iteration %d: expected INSERT, got %q", i, insertSQL)
		}
		if !strings.Contains(updateSQL, "UPDATE tasks") {
			t.Errorf("iteration %d: expected UPDATE, got %q", i, updateSQL)
		}
	}
}

// TestForEach_PerIterationAccumIsolation proves that sink.Output writes
// inside one iteration do not leak into sibling iterations. Each row sets
// "resp" to a different value via the api sink; the storage sink then
// writes it. If accum cloning was broken, all iterations would see the
// LAST iteration's resp value.
func TestForEach_PerIterationAccumIsolation(t *testing.T) {
	resetFns(t)

	// Upstream returns the path back — gives each iteration a unique value.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"echo":"` + path + `"}`))
	}))
	defer upstream.Close()

	reg, _ := httpclient.NewRegistry(map[string]*httpclient.RequestDef{
		"echo": {URL: upstream.URL + "/{{name}}", Method: "GET"},
	})
	httpclient.SetDefault(reg)

	writes := &recordingStorage{}
	GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
		return writes, name == "db"
	}

	accum := map[string]any{
		"items": []any{
			map[string]any{"name": "first"},
			map[string]any{"name": "second"},
			map[string]any{"name": "third"},
		},
	}
	sinks := []*Sink{
		{
			Type: "for_each",
			In:   "items",
			As:   "item",
			Do: []*Sink{
				{Type: "api", Ref: "echo", Vars: map[string]string{"name": "item.name"}, Output: "resp"},
				{Type: "storage", Source: "db",
					// Parsed JSON lives under resp.json — explicit, no implicit merge.
					Inputs:  map[string]string{"echo": "resp.json.echo"},
					Execute: "INSERT INTO log(echo) VALUES({{echo}})"},
			},
		},
	}
	if err := ApplySinks(context.Background(), sinks, accum); err != nil {
		t.Fatalf("ApplySinks: %v", err)
	}

	if len(writes.calls) != 3 {
		t.Fatalf("expected 3 storage writes, got %d", len(writes.calls))
	}

	// Inspect the bound SQL params via the DataLoader. Each iteration
	// should have written its own iteration's "echo" value, not the last
	// iteration's. We probe the DataLoader by re-rendering a tiny SQL
	// template through it and inspecting what it bound.
	expected := []string{"first", "second", "third"}
	for i, want := range expected {
		dl := writes.calls[i].dl
		got, err := dl.GetValue("echo")
		if err != nil {
			t.Errorf("iter %d: GetValue(echo): %v", i, err)
			continue
		}
		if got != want {
			t.Errorf("iter %d: expected echo=%q, got %q — accum cloning is broken", i, want, got)
		}
	}

	// Also verify outer accum was NOT mutated by any iteration.
	if _, ok := accum["resp"]; ok {
		t.Error("outer accum has 'resp' — for_each leaked iteration writes upward")
	}
	if _, ok := accum["item"]; ok {
		t.Error("outer accum has 'item' — for_each leaked iteration variable upward")
	}
}

// TestApplySinks_OnErrorContinue verifies the on_error: continue policy:
// a sink whose error would normally abort the tick (here a for_each over a
// missing accumulator path — the exact Atom-vs-RSS failure) is logged and
// skipped, and sibling sinks still run. Without on_error it must still error.
func TestApplySinks_OnErrorContinue(t *testing.T) {
	resetFns(t)

	stub := &stubStorage{result: nil}
	GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
		return stub, name == "db"
	}

	// accum deliberately lacks `r.xml.rss.channel.item`.
	accum := map[string]any{"r": map[string]any{"xml": map[string]any{}}}

	missingForEach := func(onErr string) *Sink {
		return &Sink{
			Type: "for_each", In: "r.xml.rss.channel.item", As: "it",
			OnError: onErr,
			Do:      []*Sink{{Type: "publish", Connection: "events"}},
		}
	}
	sibling := &Sink{
		Type: "storage", Source: "db",
		Execute: "INSERT INTO t DEFAULT VALUES",
		Inputs:  map[string]string{},
	}

	// With on_error: continue → no error, sibling still executes.
	if err := ApplySinks(context.Background(),
		[]*Sink{missingForEach("continue"), sibling}, accum); err != nil {
		t.Fatalf("on_error=continue should swallow the error, got: %v", err)
	}
	if stub.capturedSQL != "INSERT INTO t DEFAULT VALUES" {
		t.Fatalf("sibling sink did not run after skipped for_each (capturedSQL=%q)", stub.capturedSQL)
	}

	// Default (no on_error) → the error propagates (tick aborts).
	stub.capturedSQL = ""
	if err := ApplySinks(context.Background(),
		[]*Sink{missingForEach(""), sibling}, accum); err == nil {
		t.Fatal("without on_error the missing-path for_each must return an error")
	}
	if stub.capturedSQL != "" {
		t.Fatal("sibling sink ran despite an aborting for_each error")
	}
}
