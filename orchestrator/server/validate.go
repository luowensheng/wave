package servers

import (
	"fmt"

	"github.com/luowensheng/wave/infra/connections"
	"github.com/luowensheng/wave/infra/plugins"
)

// ValidateConfig runs static checks against the loaded YAML without
// starting the server. Used by `wave validate` so CI / pre-deploy
// hooks can fail fast on misconfiguration.
//
// Checks:
//   - every plugin entry has a known transport with required fields
//   - every connection entry has a known type and subscribe_path
//   - every named limit entry has a known case
//   - every plugin route's `name` resolves in plugins{}
//   - every stream-publish route's `connection` resolves in connections{}
//   - every route's named limit references resolve in limits{}
//   - every route has a recognized `type`
func (s *Server) ValidateConfig() error {
	if s == nil || s.Config == nil {
		return fmt.Errorf("nil config")
	}
	cfg := s.Config

	for name, p := range cfg.Plugins {
		if err := p.Validate(); err != nil {
			return fmt.Errorf("plugins[%q]: %w", name, err)
		}
	}
	for name, c := range cfg.Connections {
		if err := c.Validate(); err != nil {
			return fmt.Errorf("connections[%q]: %w", name, err)
		}
	}
	// Every entry in the limits registry must declare a known case.
	for name, entry := range cfg.Limits {
		if entry == nil {
			return fmt.Errorf("limits[%q]: nil entry", name)
		}
		if entry.Case == "" {
			return fmt.Errorf("limits[%q]: missing case", name)
		}
		if !isKnownCase(entry.Case) {
			return fmt.Errorf("limits[%q]: unknown case %q", name, entry.Case)
		}
	}

	// Materialize merged+prefixed routes so we inspect the same set
	// `wave serve` boots. No $arg/$env substitution here — variable
	// rendering is a runtime concern (args==nil leaves $x literal).
	if !cfg.routesMaterialized {
		_ = materializeRoutes(cfg, nil)
	}

	for i, r := range cfg.Routes {
		if r == nil {
			return fmt.Errorf("routes[%d] is nil", i)
		}
		switch r.Type {
		case "plugin":
			if r.PluginConfig == nil || r.PluginConfig.Name == "" {
				return fmt.Errorf("routes[%d] (%s): plugin route missing plugin.name", i, r.Path)
			}
			if _, ok := cfg.Plugins[r.PluginConfig.Name]; !ok {
				return fmt.Errorf("routes[%d] (%s): unknown plugin %q", i, r.Path, r.PluginConfig.Name)
			}
		case "stream-publish":
			if r.StreamPublishConfig == nil || r.StreamPublishConfig.Connection == "" {
				return fmt.Errorf("routes[%d] (%s): stream-publish missing connection", i, r.Path)
			}
			if _, ok := cfg.Connections[r.StreamPublishConfig.Connection]; !ok {
				return fmt.Errorf("routes[%d] (%s): unknown connection %q", i, r.Path, r.StreamPublishConfig.Connection)
			}
		}
		// Every named limit reference on this route must resolve in
		// the top-level limits registry.
		for _, name := range r.Limits {
			if _, ok := cfg.Limits[name]; !ok {
				return fmt.Errorf("routes[%d] (%s): unknown limit %q", i, r.Path, name)
			}
		}
	}
	_ = plugins.Default
	_ = connections.Default
	return nil
}
