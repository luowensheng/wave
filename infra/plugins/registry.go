package plugins

import (
	"fmt"
	"sync"
)

// Registry is a name-keyed lookup of plugin clients, built once at server
// boot from the YAML `plugins:` block.
type Registry struct {
	mu      sync.RWMutex
	clients map[string]Client
	kinds   map[string]string // name → effective Kind (handler default)
}

// NewRegistry constructs and validates a Registry from configs.
// Returns an error on the first config that fails validation, so the
// server fails fast at boot rather than at request time.
func NewRegistry(configs map[string]*PluginConfig) (*Registry, error) {
	reg := &Registry{
		clients: make(map[string]Client, len(configs)),
		kinds:   make(map[string]string, len(configs)),
	}
	for name, cfg := range configs {
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("plugin %q: %w", name, err)
		}
		client, err := buildClient(name, cfg)
		if err != nil {
			return nil, fmt.Errorf("plugin %q: %w", name, err)
		}
		// Decoration order matters: retry wraps the raw transport so each
		// metrics-recorded "call" represents a logical user-facing call
		// (potentially several internal attempts), and so total latency
		// includes the backoff sleeps.
		client = wrapWithRetry(client, cfg)
		reg.clients[name] = wrapWithMetrics(name, client)
		reg.kinds[name] = EffectiveKind(cfg.Kind)
	}
	return reg, nil
}

// NamesOfKind returns every plugin name registered with the given kind.
// Empty kind matches handlers (back-compat with configs missing `kind:`).
func (r *Registry) NamesOfKind(kind string) []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	want := EffectiveKind(kind)
	out := make([]string, 0, len(r.kinds))
	for name, k := range r.kinds {
		if k == want {
			out = append(out, name)
		}
	}
	return out
}

// Get returns the client registered under name, or false.
func (r *Registry) Get(name string) (Client, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c, ok := r.clients[name]
	return c, ok
}

// Close releases every transport.
func (r *Registry) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	var firstErr error
	for _, c := range r.clients {
		if err := c.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// buildClient picks the transport implementation for the given config.
//
// For Transport=="process" the choice depends on Kind: handler-kind
// (or empty Kind) uses the legacy one-shot subprocess (one fork per
// Call) for back-compat; non-handler kinds use the long-lived JSON-RPC
// transport so the plugin can hold connection pools and cached state.
func buildClient(name string, cfg *PluginConfig) (Client, error) {
	switch cfg.Transport {
	case "process":
		if EffectiveKind(cfg.Kind) == KindHandler {
			return newSubprocessClient(cfg), nil
		}
		return newLongLivedClient(cfg, name), nil
	case "http":
		return newHTTPClient(cfg), nil
	case "grpc":
		return newGRPCStub(cfg), nil
	case "wasm":
		return newWASMStub(cfg), nil
	default:
		return nil, fmt.Errorf("unknown transport %q", cfg.Transport)
	}
}

// ── default global registry, set at boot ──────────────────────────────────

var (
	defaultMu sync.RWMutex
	defaultR  *Registry
)

// SetDefault publishes a registry that any package (e.g. usecases/plugin)
// can look up by name. Called from server boot.
func SetDefault(r *Registry) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultR = r
}

// Default returns the registry installed by SetDefault, or nil.
func Default() *Registry {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultR
}
