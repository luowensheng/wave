package sdk

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type echo struct{}

func (echo) Call(_ context.Context, req *Request) (*Response, error) {
	body, _ := json.Marshal(map[string]string{"trigger": req.TriggerKey})
	return &Response{Status: 200, Body: body}, nil
}
func (echo) Close() error { return nil }

// pipeWriter is a small thread-safe collector for the serve loop's output.
type safeBuf struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *safeBuf) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}
func (b *safeBuf) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestRunHandlerSmoke(t *testing.T) {
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"handler.call","params":{"trigger_key":"k"}}` + "\n" +
			`{"jsonrpc":"2.0","method":"shutdown"}` + "\n",
	)
	out := &safeBuf{}
	done := make(chan error, 1)
	handlers := map[string]methodFn{
		MethodHandlerCall: func(ctx context.Context, raw json.RawMessage) (any, error) {
			var req Request
			_ = json.Unmarshal(raw, &req)
			return echo{}.Call(ctx, &req)
		},
	}
	go func() { done <- serveOn(in, out, handlers, func() error { return nil }) }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not exit on shutdown")
	}
	if !strings.Contains(out.String(), `"trigger":"k"`) {
		t.Errorf("missing echoed body: %s", out.String())
	}
}

func TestServeUnknownMethod(t *testing.T) {
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"nope"}` + "\n" +
			`{"jsonrpc":"2.0","method":"shutdown"}` + "\n",
	)
	out := &safeBuf{}
	_ = serveOn(in, out, map[string]methodFn{}, nil)
	if !strings.Contains(out.String(), "method not found") {
		t.Errorf("expected method not found error, got %s", out.String())
	}
}

func TestServePanicReturnsError(t *testing.T) {
	in := strings.NewReader(
		`{"jsonrpc":"2.0","id":1,"method":"boom"}` + "\n" +
			`{"jsonrpc":"2.0","method":"shutdown"}` + "\n",
	)
	out := &safeBuf{}
	handlers := map[string]methodFn{
		"boom": func(ctx context.Context, raw json.RawMessage) (any, error) {
			panic("argh")
		},
	}
	_ = serveOn(in, out, handlers, nil)
	// Allow async write.
	time.Sleep(50 * time.Millisecond)
	if !strings.Contains(out.String(), "plugin panic") {
		t.Errorf("expected panic error, got %s", out.String())
	}
}

// Sanity check: WriteManifest produces valid JSON.
func TestWriteManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/manifest.json"
	if err := WriteManifest(path, &Manifest{Name: "x", Kind: "handler", Version: "1"}); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatal(err)
	}
	if m.Name != "x" {
		t.Fail()
	}
}
