package servers

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/luowensheng/wave/infra/connections"
	"github.com/luowensheng/wave/orchestrator/features/auth"
	"github.com/luowensheng/wave/infra/httpclient"
	"github.com/luowensheng/wave/infra/plugins"
	"github.com/luowensheng/wave/infra/secrets"
	"github.com/luowensheng/wave/orchestrator/features/storage"

	"gopkg.in/yaml.v3"
)

// libKinds is the frozen set of resource kinds a typed library may
// declare. The kind replaces the wrapper key: a `kind: storage`
// library lists storage entries at the file root.
var libKinds = map[string]bool{
	"storage":     true,
	"plugins":     true,
	"connections": true,
	"auth":        true,
	"requests":    true,
	"limits":      true,
}

// typedLibrary is a parsed `kind:` resource library. It owns exactly
// one kind's resources, keyed by name, with the raw YAML node kept for
// strict body-conflict comparison. abs is the canonical identity root.
type typedLibrary struct {
	abs   string
	kind  string
	nodes map[string]yaml.Node
}

// libraryCache memoizes loadTypedLibrary by absolute path for the
// lifetime of a single resolve. Same library borrowed N times parses
// once and yields one identity.
type libraryCache struct {
	byAbs map[string]*typedLibrary
}

func newLibraryCache() *libraryCache {
	return &libraryCache{byAbs: map[string]*typedLibrary{}}
}

// resolveAbs makes ref absolute relative to baseDir (the directory of
// the file that declared ref), independent of the process CWD, then
// canonicalizes symlinks. Identity is derived from this value.
func resolveAbs(baseDir, ref string) (string, error) {
	p := ref
	if !filepath.IsAbs(p) {
		p = filepath.Join(baseDir, ref)
	}
	abs, err := filepath.Abs(p)
	if err != nil {
		return "", fmt.Errorf("resolve path %q (from %q): %w", ref, baseDir, err)
	}
	if real, err := filepath.EvalSymlinks(abs); err == nil {
		abs = real
	}
	return abs, nil
}

// libraryProbe reads only the structural keys needed to validate that
// a file is a well-formed typed library (kind set, no server-only
// blocks). The actual resource nodes are captured separately so the
// loader stays format-agnostic (JSON is a YAML 1.2 subset).
type libraryProbe struct {
	Kind     string               `yaml:"kind"`
	Routes   yaml.Node            `yaml:"routes"`
	Default  yaml.Node            `yaml:"default"`
	Include  yaml.Node            `yaml:"include"`
	Args     yaml.Node            `yaml:"args"`
	Env      yaml.Node            `yaml:"env"`
	Resource map[string]yaml.Node `yaml:",inline"`
}

func nodePresent(n yaml.Node) bool { return n.Kind != 0 }

// loadTypedLibrary loads, validates, and caches a typed resource
// library by its canonical absolute path. Parsing is format-agnostic:
// yaml.Unmarshal handles .json and .yaml/.yml identically — identity
// is the resolved path, never the extension.
func (c *libraryCache) loadTypedLibrary(abs string) (*typedLibrary, error) {
	if lib, ok := c.byAbs[abs]; ok {
		return lib, nil
	}

	raw, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("extern target %s: cannot read library file: %w (fix: check the extern path is correct and the file exists)", abs, err)
	}
	expanded, err := secrets.Expand(string(raw))
	if err != nil {
		return nil, fmt.Errorf("extern target %s: failed to resolve secrets: %w", abs, err)
	}

	var probe libraryProbe
	if err := yaml.Unmarshal([]byte(expanded), &probe); err != nil {
		return nil, fmt.Errorf("extern target %s: failed to parse library: %w", abs, err)
	}

	if probe.Kind == "" {
		return nil, fmt.Errorf("extern target %s: missing top-level `kind:` (fix: a library must declare `kind: <storage|plugins|connections|auth|requests|limits>` at its root)", abs)
	}
	if !libKinds[probe.Kind] {
		return nil, fmt.Errorf("extern target %s: invalid kind %q (fix: use one of storage, plugins, connections, auth, requests, limits)", abs, probe.Kind)
	}
	if nodePresent(probe.Routes) {
		return nil, fmt.Errorf("extern target %s: kind:%s library must not declare `routes:` (fix: libraries contain only %s resources — move routes to a module)", abs, probe.Kind, probe.Kind)
	}
	if nodePresent(probe.Default) {
		return nil, fmt.Errorf("extern target %s: kind:%s library must not declare `default:` (fix: libraries contain only %s resources)", abs, probe.Kind, probe.Kind)
	}
	if nodePresent(probe.Include) {
		return nil, fmt.Errorf("extern target %s: kind:%s library must not declare `include:` (fix: libraries contain only %s resources — composition belongs in the host)", abs, probe.Kind, probe.Kind)
	}
	if nodePresent(probe.Args) || nodePresent(probe.Env) {
		return nil, fmt.Errorf("extern target %s: kind:%s library must not declare `args:`/`env:` (fix: variables come from the top-level host)", abs, probe.Kind)
	}

	// The library may list its resources either at the file root (the
	// canonical form, kind replaces the wrapper) or — tolerated — under
	// the kind-named wrapper key. Anything else is a foreign block.
	nodes := map[string]yaml.Node{}
	for k, v := range probe.Resource {
		if k == probe.Kind {
			var inner map[string]yaml.Node
			if err := v.Decode(&inner); err != nil {
				return nil, fmt.Errorf("extern target %s: kind:%s wrapper is not a map: %w", abs, probe.Kind, err)
			}
			for ik, iv := range inner {
				nodes[ik] = iv
			}
			continue
		}
		if libKinds[k] {
			return nil, fmt.Errorf("extern target %s: kind:%s library must not also declare %q resources (fix: a library contains only its own kind)", abs, probe.Kind, k)
		}
		nodes[k] = v
	}

	lib := &typedLibrary{abs: abs, kind: probe.Kind, nodes: nodes}
	c.byAbs[abs] = lib
	return lib, nil
}

// decodeResource decodes a raw resource node into the concrete typed
// pointer for the given kind. Used for both inline (host-authored) and
// extern (library-authored) entries so both paths produce identical
// Go values regardless of source format.
func decodeResource(kind string, n *yaml.Node) (any, error) {
	switch kind {
	case "storage":
		var v storage.StorageConfig
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		return &v, nil
	case "plugins":
		var v plugins.PluginConfig
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		return &v, nil
	case "connections":
		var v connections.ConnectionConfig
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		return &v, nil
	case "auth":
		var v auth.AuthConfig
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		return &v, nil
	case "requests":
		var v httpclient.RequestDef
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		return &v, nil
	case "limits":
		var v LimitEntry
		if err := n.Decode(&v); err != nil {
			return nil, err
		}
		return &v, nil
	default:
		return nil, fmt.Errorf("unknown resource kind %q", kind)
	}
}
