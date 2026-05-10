package plugins

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// PluginHealth is the latest probe result for a plugin.
type PluginHealth struct {
	Name      string
	OK        bool
	LastError string
	CheckedAt time.Time
	LatencyMS int64
}

// HealthMonitor periodically probes the configured plugins (currently
// only HTTP transports — subprocess plugins are validated at startup
// and gRPC/WASM are stubs). Results are exposed via Snapshot() so the
// admin dashboard can show a live view.
type HealthMonitor struct {
	configs map[string]*PluginConfig
	client  *http.Client

	mu     sync.RWMutex
	state  map[string]PluginHealth
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewHealthMonitor builds a monitor over the configured plugins.
func NewHealthMonitor(configs map[string]*PluginConfig) *HealthMonitor {
	return &HealthMonitor{
		configs: configs,
		client:  &http.Client{Timeout: 5 * time.Second},
		state:   make(map[string]PluginHealth),
	}
}

// Start kicks off a probe loop with the given interval. Cancel via Stop.
func (h *HealthMonitor) Start(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = 30 * time.Second
	}
	ctx, h.cancel = context.WithCancel(ctx)
	h.wg.Add(1)
	go func() {
		defer h.wg.Done()
		h.probeAll(ctx)
		t := time.NewTicker(every)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				h.probeAll(ctx)
			}
		}
	}()
}

// Stop cancels the loop and waits for the goroutine to exit.
func (h *HealthMonitor) Stop() {
	if h.cancel != nil {
		h.cancel()
	}
	h.wg.Wait()
}

func (h *HealthMonitor) probeAll(ctx context.Context) {
	for name, cfg := range h.configs {
		switch cfg.Transport {
		case "http":
			h.probeHTTP(ctx, name, cfg)
		case "process":
			// Boot-time check is sufficient; no per-probe value.
		}
	}
}

func (h *HealthMonitor) probeHTTP(ctx context.Context, name string, cfg *PluginConfig) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	start := time.Now()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, cfg.Address, nil)
	res := PluginHealth{Name: name, CheckedAt: time.Now()}
	resp, err := h.client.Do(req)
	if err != nil {
		res.OK = false
		res.LastError = err.Error()
	} else {
		_ = resp.Body.Close()
		res.OK = resp.StatusCode < 500
		if !res.OK {
			res.LastError = "status " + http.StatusText(resp.StatusCode)
		}
	}
	res.LatencyMS = time.Since(start).Milliseconds()
	h.mu.Lock()
	h.state[name] = res
	h.mu.Unlock()
}

// Snapshot returns the most recent probe result for every monitored
// plugin.
func (h *HealthMonitor) Snapshot() []PluginHealth {
	h.mu.RLock()
	defer h.mu.RUnlock()
	out := make([]PluginHealth, 0, len(h.state))
	for _, r := range h.state {
		out = append(out, r)
	}
	return out
}

// ── default global monitor ────────────────────────────────────────────────

var (
	hmu        sync.RWMutex
	defaultHM  *HealthMonitor
)

func SetDefaultHealthMonitor(h *HealthMonitor) {
	hmu.Lock()
	defer hmu.Unlock()
	defaultHM = h
}

func DefaultHealthMonitor() *HealthMonitor {
	hmu.RLock()
	defer hmu.RUnlock()
	return defaultHM
}
