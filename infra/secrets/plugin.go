package secrets

import (
	"fmt"
	"strings"
	"sync"
)

// PluginResolverFn looks up a secrets-kind plugin by name and resolves
// a plugin-specific URI to bytes. Installed via SetPluginResolver from
// the orchestrator boot sequence after the plugin registry exists.
type PluginResolverFn func(name, uri string) ([]byte, error)

var (
	pluginResolverMu sync.RWMutex
	pluginResolverFn PluginResolverFn
)

// SetPluginResolver installs the resolver used by ${PLUGIN:name:uri}
// markers and registers the PLUGIN prefix with the default resolver
// table. Until called, ${PLUGIN:...} markers are left intact by Expand
// (the unknown-prefix branch preserves them verbatim) — that is what
// makes the two-phase secret-resolution model work: the first
// Expand pass on raw YAML preserves the markers, and the second pass
// (after plugins boot) resolves them via this fn.
//
// Calling with nil clears the resolver and removes the PLUGIN prefix
// from the resolver table — useful for tests.
func SetPluginResolver(fn PluginResolverFn) {
	pluginResolverMu.Lock()
	pluginResolverFn = fn
	pluginResolverMu.Unlock()

	if fn == nil {
		// Remove the registered resolver so unknown-prefix semantics
		// return for ${PLUGIN:...} markers.
		delete(defaultResolvers, "PLUGIN")
		return
	}
	Register("PLUGIN", resolvePlugin)
}

// HasPluginResolver reports whether a plugin resolver is currently
// installed. Used by callers that want to short-circuit a second-pass
// walk if no plugins back the PLUGIN prefix.
func HasPluginResolver() bool {
	pluginResolverMu.RLock()
	defer pluginResolverMu.RUnlock()
	return pluginResolverFn != nil
}

// resolvePlugin is the Resolver wired into defaultResolvers under the
// PLUGIN key. It splits "name:uri" on the first colon — the URI may
// contain its own colons / slashes / hashes, all preserved verbatim.
func resolvePlugin(arg string) (string, error) {
	pluginResolverMu.RLock()
	fn := pluginResolverFn
	pluginResolverMu.RUnlock()
	if fn == nil {
		// Defensive: if the resolver was cleared concurrently, fall
		// through to leaving the marker intact.
		return "${PLUGIN:" + arg + "}", nil
	}
	name, uri, ok := strings.Cut(arg, ":")
	if !ok || name == "" {
		return "", fmt.Errorf("malformed PLUGIN marker %q: expected name:uri", arg)
	}
	v, err := fn(name, uri)
	if err != nil {
		return "", err
	}
	return string(v), nil
}
