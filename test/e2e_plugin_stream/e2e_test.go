// Package e2e_plugin_stream is an end-to-end smoke test exercising the
// real route dispatch with both new route types:
//   - POST /webhooks/test (type: stream-publish) → broker
//   - GET  /events/payments (auto-registered SSE subscriber) ← broker
//   - POST /echo (type: plugin, transport: process) → echo binary
package e2e_plugin_stream

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"wave/infra/connections"
	"wave/infra/plugins"
	pluginuse "wave/usecases/plugin"
	streampub "wave/usecases/stream_publish"
)

func buildEchoPlugin(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("e2e skipped on windows for shell-style invocation")
	}
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "echo")
	cmd := exec.Command("go", "build", "-o", out, "./examples/plugins/echo")
	cmd.Dir = root
	cmd.Env = os.Environ()
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build echo plugin: %v\n%s", err, b)
	}
	return out
}

func TestEndToEnd(t *testing.T) {
	echoPath := buildEchoPlugin(t)

	// 1. Plugin registry with the echo subprocess plugin.
	preg, err := plugins.NewRegistry(map[string]*plugins.PluginConfig{
		"echo": {Transport: "process", Command: echoPath, Timeout: "3s"},
	})
	if err != nil {
		t.Fatal(err)
	}
	plugins.SetDefault(preg)

	// 2. Connection registry with one SSE broker.
	creg, err := connections.NewRegistry(map[string]*connections.ConnectionConfig{
		"payments": {
			Type:              "sse",
			SubscribePath:     "/events/payments",
			BufferSize:        16,
			KeepAliveInterval: "1s",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	connections.SetDefault(creg)

	// 3. Build a tiny mux exposing all three routes.
	mux := http.NewServeMux()

	pluginCfg := &pluginuse.Config{
		Name:       "echo",
		TriggerKey: "hello",
		ResponseOutput: map[string]string{
			"echoed":  "response.echoed",
			"trigger": "response.trigger_key",
		},
	}
	pluginH, err := pluginCfg.CreateRoute("POST", "/echo", nil)
	if err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("POST /echo", pluginH)

	pubCfg := &streampub.Config{
		Connection: "payments",
		EventType:  "payment",
		Output:     map[string]string{"payment_id": "response.id", "amount": "response.amount"},
		StaticMeta: map[string]string{"source": "stripe"},
	}
	pubH, err := pubCfg.CreateRoute("POST", "/webhooks/test", nil)
	if err != nil {
		t.Fatal(err)
	}
	mux.HandleFunc("POST /webhooks/test", pubH)

	mux.HandleFunc("GET /events/payments", func(w http.ResponseWriter, r *http.Request) {
		broker, _ := creg.Get("payments")
		connections.ServeSSE(w, r, broker)
	})

	srv := httptest.NewServer(mux)
	defer srv.Close()

	// 4. Open SSE stream.
	resp, err := http.Get(srv.URL + "/events/payments")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("subscribe status = %d", resp.StatusCode)
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)

	type sseFrame struct {
		event string
		data  string
	}
	frames := make(chan sseFrame, 4)
	go func() {
		var cur sseFrame
		for scanner.Scan() {
			line := scanner.Text()
			switch {
			case strings.HasPrefix(line, ": "):
				continue
			case strings.HasPrefix(line, "event: "):
				cur.event = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				cur.data = strings.TrimPrefix(line, "data: ")
			case line == "":
				if cur.data != "" {
					frames <- cur
					cur = sseFrame{}
				}
			}
		}
	}()

	// 5. Publish a webhook.
	payload := bytes.NewReader([]byte(`{"id":"pi_123","amount":2000,"secret":"sk_x"}`))
	pr, err := http.Post(srv.URL+"/webhooks/test", "application/json", payload)
	if err != nil {
		t.Fatal(err)
	}
	pr.Body.Close()
	if pr.StatusCode != http.StatusAccepted {
		t.Fatalf("publish status = %d", pr.StatusCode)
	}

	select {
	case f := <-frames:
		if f.event != "payment" {
			t.Errorf("event = %q", f.event)
		}
		var got map[string]any
		if err := json.Unmarshal([]byte(f.data), &got); err != nil {
			t.Fatalf("decode data: %v (data=%s)", err, f.data)
		}
		if got["payment_id"] != "pi_123" {
			t.Errorf("payment_id = %v", got["payment_id"])
		}
		if got["source"] != "stripe" {
			t.Errorf("source = %v", got["source"])
		}
		if _, leaked := got["secret"]; leaked {
			t.Errorf("secret leaked: %v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for SSE frame")
	}

	// 6. Plugin route round-trip via real subprocess.
	echoResp, err := http.Post(srv.URL+"/echo", "application/json", strings.NewReader(`{"hi":1}`))
	if err != nil {
		t.Fatal(err)
	}
	defer echoResp.Body.Close()
	if echoResp.StatusCode != 200 {
		t.Fatalf("echo status = %d", echoResp.StatusCode)
	}
	var echoBody map[string]any
	if err := json.NewDecoder(echoResp.Body).Decode(&echoBody); err != nil {
		t.Fatalf("decode echo: %v", err)
	}
	if fmt.Sprint(echoBody["trigger"]) != "hello" {
		t.Errorf("trigger = %v", echoBody["trigger"])
	}
	echoed, _ := echoBody["echoed"].(map[string]any)
	if v, _ := echoed["hi"].(float64); v != 1 {
		t.Errorf("echoed.hi = %v", echoBody["echoed"])
	}
}
