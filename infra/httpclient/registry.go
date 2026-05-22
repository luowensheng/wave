package httpclient

import "fmt"

// Registry holds named RequestDef entries.
type Registry struct {
	defs map[string]*RequestDef
}

// NewRegistry builds a Registry from a raw map, resolving any file: references.
// Inline fields override the file-loaded definition via Merge.
func NewRegistry(raw map[string]*RequestDef) (*Registry, error) {
	resolved := make(map[string]*RequestDef, len(raw))
	for name, def := range raw {
		if def.File != "" {
			base, err := LoadFromFile(def.File)
			if err != nil {
				return nil, fmt.Errorf("request %q: %w", name, err)
			}
			def = Merge(base, def)
			def.File = "" // clear after resolution
		}
		resolved[name] = def
	}
	return &Registry{defs: resolved}, nil
}

// Get returns the named RequestDef, or false if not found.
func (r *Registry) Get(name string) (*RequestDef, bool) {
	if r == nil {
		return nil, false
	}
	def, ok := r.defs[name]
	return def, ok
}

var defaultRegistry *Registry

// SetDefault sets the process-wide default registry.
func SetDefault(r *Registry) { defaultRegistry = r }

// Default returns the process-wide default registry.
func Default() *Registry { return defaultRegistry }
