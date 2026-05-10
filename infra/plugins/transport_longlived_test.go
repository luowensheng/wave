package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestMain lets us reuse the test binary as the plugin executable. When
// invoked with WAVE_PLUGIN_TEST_MODE set, we run the inline plugin
// loop and exit without running any tests.
func TestMain(m *testing.M) {
	if mode := os.Getenv("WAVE_PLUGIN_TEST_MODE"); mode != "" {
		runFakePlugin(mode)
		return
	}
	os.Exit(m.Run())
}

// runFakePlugin reads JSON-RPC frames from stdin and writes responses to
// stdout. Modes:
//
//	echo    — every request returns {"echoed": params}
//	panic   — first call panics, subsequent calls echo
//	crash   — exits 1 after the first call
//	slow    — sleeps 200ms before responding (for ordering tests)
func runFakePlugin(mode string) {
	dec := json.NewDecoder(os.Stdin)
	var mu sync.Mutex
	write := func(v any) {
		buf, _ := json.Marshal(v)
		buf = append(buf, '\n')
		mu.Lock()
		_, _ = os.Stdout.Write(buf)
		mu.Unlock()
	}
	calls := 0
	for {
		var req struct {
			JSONRPC string          `json:"jsonrpc"`
			ID      *uint64         `json:"id,omitempty"`
			Method  string          `json:"method"`
			Params  json.RawMessage `json:"params,omitempty"`
		}
		if err := dec.Decode(&req); err != nil {
			os.Exit(0)
		}
		if req.Method == "shutdown" {
			os.Exit(0)
		}
		calls++
		if mode == "panic" && calls == 1 {
			write(map[string]any{
				"jsonrpc": "2.0",
				"id":      req.ID,
				"error":   map[string]any{"code": -32603, "message": "boom"},
			})
			continue
		}
		if mode == "slow" {
			go func(id *uint64, params json.RawMessage, n int) {
				time.Sleep(200 * time.Millisecond)
				write(map[string]any{
					"jsonrpc": "2.0",
					"id":      id,
					"result":  map[string]any{"echoed": json.RawMessage(params), "n": n},
				})
			}(req.ID, req.Params, calls)
			continue
		}
		write(map[string]any{
			"jsonrpc": "2.0",
			"id":      req.ID,
			"result":  map[string]any{"echoed": json.RawMessage(req.Params), "n": calls},
		})
		if mode == "crash" {
			// Exit *after* sending the first response so the test can
			// observe the unexpected exit and restart bookkeeping.
			os.Exit(1)
		}
	}
}

func selfBin(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("test self-binary trick is POSIX-flavoured")
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return exe
}

func newPluginCfg(t *testing.T, mode string) *PluginConfig {
	t.Helper()
	bin := selfBin(t)
	cfg := &PluginConfig{
		Transport: "process",
		Kind:      KindStorage,
		Command:   bin,
		Timeout:   "5s",
		Env:       map[string]string{"WAVE_PLUGIN_TEST_MODE": mode},
	}
	return cfg
}

func TestLongLivedRoundTrip(t *testing.T) {
	cfg := newPluginCfg(t, "echo")
	c := newLongLivedClient(cfg, "test-echo").(*longLivedClient)
	defer c.Close()

	raw, err := c.RPC(context.Background(), "storage.get", map[string]string{"key": "k"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `"echoed"`) {
		t.Errorf("unexpected result: %s", raw)
	}
}

func TestLongLivedConcurrent(t *testing.T) {
	cfg := newPluginCfg(t, "slow")
	c := newLongLivedClient(cfg, "test-slow").(*longLivedClient)
	defer c.Close()

	const N = 8
	var wg sync.WaitGroup
	errs := make(chan error, N)
	start := time.Now()
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := c.RPC(context.Background(), "storage.get",
				map[string]any{"key": fmt.Sprintf("k%d", i)})
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Errorf("call err: %v", err)
		}
	}
	// 8 concurrent ~200ms calls should finish well under 8*200ms if
	// the transport doesn't serialize them.
	if d := time.Since(start); d > 1500*time.Millisecond {
		t.Errorf("concurrency broken: %s", d)
	}
}

func TestLongLivedShutdownGraceful(t *testing.T) {
	cfg := newPluginCfg(t, "echo")
	c := newLongLivedClient(cfg, "test-shutdown").(*longLivedClient)
	if _, err := c.RPC(context.Background(), "storage.get", map[string]string{"key": "k"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestLongLivedRpcError(t *testing.T) {
	cfg := newPluginCfg(t, "panic")
	c := newLongLivedClient(cfg, "test-panic").(*longLivedClient)
	defer c.Close()
	_, err := c.RPC(context.Background(), "storage.get", map[string]string{"key": "k"})
	if err == nil {
		t.Fatal("expected error from plugin")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestLongLivedRestartOnExit(t *testing.T) {
	cfg := newPluginCfg(t, "crash")
	c := newLongLivedClient(cfg, "test-crash").(*longLivedClient)
	defer c.Close()

	// First call succeeds, then plugin exits before the next call.
	if _, err := c.RPC(context.Background(), "storage.get", map[string]string{"key": "1"}); err != nil {
		t.Fatal(err)
	}
	// Wait for the wait goroutine to notice the exit.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		c.mu.Lock()
		gone := c.proc == nil
		c.mu.Unlock()
		if gone {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	// Next call should restart the subprocess.
	if _, err := c.RPC(context.Background(), "storage.get", map[string]string{"key": "2"}); err != nil {
		t.Fatalf("restart call failed: %v", err)
	}
}
