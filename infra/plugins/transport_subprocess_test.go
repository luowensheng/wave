package plugins

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// writeShellPlugin drops a tiny shell plugin to disk and returns its path.
// It echoes a fixed JSON response, ignoring stdin.
func writeShellPlugin(t *testing.T, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell plugin test skipped on windows")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "plugin.sh")
	script := "#!/bin/sh\ncat > /dev/null\nprintf '%s' '" + strings.ReplaceAll(body, "'", "'\\''") + "'\n"
	if err := os.WriteFile(p, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestSubprocessClientRoundTrip(t *testing.T) {
	p := writeShellPlugin(t, `{"status":200,"headers":{"X-A":"1"},"body":{"ok":true}}`)

	cfg := &PluginConfig{Transport: "process", Command: p, Timeout: "3s"}
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	c := newSubprocessClient(cfg)
	defer c.Close()

	resp, err := c.Call(context.Background(), &Request{
		TriggerKey: "t",
		Body:       json.RawMessage(`{"x":1}`),
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Status != 200 {
		t.Errorf("status = %d", resp.Status)
	}
	if resp.Headers["X-A"] != "1" {
		t.Errorf("header missing: %v", resp.Headers)
	}
	if !strings.Contains(string(resp.Body), `"ok":true`) {
		t.Errorf("body = %s", resp.Body)
	}
}

func TestSubprocessTimeout(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell test skipped on windows")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "slow.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\nsleep 5\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := &PluginConfig{Transport: "process", Command: p, Timeout: "100ms"}
	c := newSubprocessClient(cfg)
	defer c.Close()
	if _, err := c.Call(context.Background(), &Request{}); err == nil {
		t.Error("expected timeout error")
	}
}

func TestSplitCommand(t *testing.T) {
	cases := []struct {
		in   string
		want []string
		err  bool
	}{
		{"./echo", []string{"./echo"}, false},
		{`./run --mode=verify`, []string{"./run", "--mode=verify"}, false},
		{`./run "a b" c`, []string{"./run", "a b", "c"}, false},
		{`./run "open`, nil, true},
	}
	for _, c := range cases {
		got, err := splitCommand(c.in)
		if c.err {
			if err == nil {
				t.Errorf("splitCommand(%q): expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitCommand(%q): %v", c.in, err)
			continue
		}
		if strings.Join(got, "|") != strings.Join(c.want, "|") {
			t.Errorf("splitCommand(%q) = %v want %v", c.in, got, c.want)
		}
	}
}

func TestRegistryFailsFastOnBadConfig(t *testing.T) {
	_, err := NewRegistry(map[string]*PluginConfig{
		"bad": {Transport: "process"}, // missing command
	})
	if err == nil {
		t.Error("expected error for missing command")
	}
	_, err = NewRegistry(map[string]*PluginConfig{
		"bad": {Transport: "nonsense"},
	})
	if err == nil {
		t.Error("expected error for unknown transport")
	}
}
