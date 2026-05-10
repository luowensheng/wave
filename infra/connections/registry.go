package connections

import (
	"fmt"
	"sync"
)

// Registry holds every named connection broker. Built once at boot.
type Registry struct {
	mu      sync.RWMutex
	brokers map[string]*Broker
}

// NewRegistry validates the configs and constructs one Broker per entry.
// Fails fast at boot on unknown types or missing subscribe paths.
func NewRegistry(configs map[string]*ConnectionConfig) (*Registry, error) {
	reg := &Registry{brokers: make(map[string]*Broker, len(configs))}
	for name, cfg := range configs {
		if err := cfg.Validate(); err != nil {
			return nil, fmt.Errorf("connection %q: %w", name, err)
		}
		reg.brokers[name] = NewBroker(cfg)
	}
	return reg, nil
}

// Get returns the broker registered under name, or false.
func (r *Registry) Get(name string) (*Broker, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	b, ok := r.brokers[name]
	return b, ok
}

// All returns the live broker map, name → broker. Caller must not mutate.
func (r *Registry) All() map[string]*Broker {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make(map[string]*Broker, len(r.brokers))
	for k, v := range r.brokers {
		out[k] = v
	}
	return out
}

// ── default global registry, set at boot ──────────────────────────────────

var (
	defaultMu sync.RWMutex
	defaultR  *Registry
)

func SetDefault(r *Registry) {
	defaultMu.Lock()
	defer defaultMu.Unlock()
	defaultR = r
}

func Default() *Registry {
	defaultMu.RLock()
	defer defaultMu.RUnlock()
	return defaultR
}
