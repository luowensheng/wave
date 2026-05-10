package plugins

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestHealthMonitorReportsHTTPProbeResult(t *testing.T) {
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	defer good.Close()
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", 503)
	}))
	defer bad.Close()

	hm := NewHealthMonitor(map[string]*PluginConfig{
		"good": {Transport: "http", Address: good.URL},
		"bad":  {Transport: "http", Address: bad.URL},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hm.Start(ctx, 50*time.Millisecond)
	defer hm.Stop()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		snap := hm.Snapshot()
		if len(snap) == 2 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := map[string]PluginHealth{}
	for _, r := range hm.Snapshot() {
		got[r.Name] = r
	}
	if !got["good"].OK {
		t.Errorf("good plugin should be OK: %+v", got["good"])
	}
	if got["bad"].OK {
		t.Errorf("bad plugin should not be OK: %+v", got["bad"])
	}
}

func TestHealthMonitorIgnoresProcessTransport(t *testing.T) {
	hm := NewHealthMonitor(map[string]*PluginConfig{
		"local": {Transport: "process", Command: "/bin/echo"},
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	hm.Start(ctx, 50*time.Millisecond)
	defer hm.Stop()
	time.Sleep(150 * time.Millisecond)
	if n := len(hm.Snapshot()); n != 0 {
		t.Errorf("subprocess plugins should not appear in health snapshot, got %d", n)
	}
}
