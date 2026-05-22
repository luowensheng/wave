// Package servers — secrets_plugin wires the secrets-kind plugins into
// the second-pass ${PLUGIN:name:uri} resolution that runs against the
// already-parsed Config struct, after plugins boot but before downstream
// features start consuming config values. See docs/secrets-plugins.md.
package servers

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/luowensheng/wave/infra/plugins"
	"github.com/luowensheng/wave/infra/plugins/kinds"
	"github.com/luowensheng/wave/infra/secrets"
)

// secretsResolveTimeout bounds individual plugin Resolve calls during
// the second-pass walk. Plugins talking to remote backends (Vault, AWS
// SM, ...) shouldn't block boot indefinitely on a slow network.
const secretsResolveTimeout = 10 * time.Second

// installSecretsPluginResolver registers the second-pass resolver and
// expands every ${PLUGIN:...} marker found in the parsed Config. After
// the walk returns, any remaining markers are surfaced as a boot-time
// error — that catches typos in plugin names early instead of letting
// them leak into runtime failures.
func (s *Server) installSecretsPluginResolver() error {
	reg := plugins.Default()
	secretsPlugins := kinds.LoadSecrets(reg)

	if len(secretsPlugins) == 0 {
		// No secrets-kind plugins configured. We still need to verify
		// that the config doesn't reference any — otherwise the
		// markers would silently survive and downstream features would
		// see literal "${PLUGIN:..." strings.
		if remaining := secrets.FindMarkers(s.Config, "PLUGIN"); len(remaining) > 0 {
			return fmt.Errorf("config references %d ${PLUGIN:...} marker(s) but no secrets-kind plugins are configured: %s",
				len(remaining), strings.Join(unique(remaining), ", "))
		}
		return nil
	}

	secrets.SetPluginResolver(func(name, uri string) ([]byte, error) {
		p, ok := secretsPlugins[name]
		if !ok {
			return nil, fmt.Errorf("unknown secrets plugin %q", name)
		}
		ctx, cancel := context.WithTimeout(context.Background(), secretsResolveTimeout)
		defer cancel()
		return p.Resolve(ctx, uri)
	})

	if err := secrets.ExpandStruct(s.Config); err != nil {
		return fmt.Errorf("expand ${PLUGIN:...} secret references: %w", err)
	}

	if remaining := secrets.FindMarkers(s.Config, "PLUGIN"); len(remaining) > 0 {
		return fmt.Errorf("unresolved ${PLUGIN:...} marker(s) after second-pass expansion (likely a typo in the plugin name): %s",
			strings.Join(unique(remaining), ", "))
	}
	return nil
}

func unique(in []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(in))
	for _, s := range in {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}
