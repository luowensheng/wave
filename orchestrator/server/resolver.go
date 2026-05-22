package servers

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/luowensheng/wave/infra/connections"
	"github.com/luowensheng/wave/orchestrator/features/auth"
	"github.com/luowensheng/wave/infra/httpclient"
	"github.com/luowensheng/wave/infra/plugins"
	"github.com/luowensheng/wave/infra/secrets"
	"github.com/luowensheng/wave/orchestrator/features/storage"

	"gopkg.in/yaml.v3"
)

// maxIncludeDepth bounds recursive include composition (design
// invariant #4). Exceeding it is an explicit error, never a stack
// overflow.
const maxIncludeDepth = 32

// UnmarshalYAML performs the two-phase decode. yaml.v3 cannot populate
// unexported fields, so a custom unmarshaler is required: phase one
// decodes every public field via a shadow alias (the alias drops the
// six resource keys so they don't conflict); phase two captures the
// raw pre-extern resource nodes into the private rawX maps for the
// resolver. Downstream code keeps reading the typed Storage/Auth/...
// maps unchanged — they are populated later by the resolver.
func (c *Config) UnmarshalYAML(value *yaml.Node) error {
	type configAlias Config
	var alias configAlias
	if err := value.Decode(&alias); err != nil {
		return err
	}
	*c = Config(alias)

	var rawResources struct {
		Storage     map[string]yaml.Node `yaml:"storage"`
		Auth        map[string]yaml.Node `yaml:"auth"`
		Plugins     map[string]yaml.Node `yaml:"plugins"`
		Connections map[string]yaml.Node `yaml:"connections"`
		Requests    map[string]yaml.Node `yaml:"requests"`
		Limits      map[string]yaml.Node `yaml:"limits"`
	}
	if err := value.Decode(&rawResources); err != nil {
		return err
	}
	c.rawStorage = rawResources.Storage
	c.rawAuth = rawResources.Auth
	c.rawPlugins = rawResources.Plugins
	c.rawConnections = rawResources.Connections
	c.rawRequests = rawResources.Requests
	c.rawLimits = rawResources.Limits
	return nil
}

// resourceKinds lists the six borrowable/composable resource kinds in
// the fixed order the resolver processes them.
var resourceKinds = []string{"storage", "plugins", "connections", "auth", "requests", "limits"}

// resIdentity is the dedup key value: which file authored a given
// (kind,name). Two different authors of the same name is a hard error
// even when bodies are identical (ambiguous ownership).
type resIdentity struct {
	abs  string // canonical authoring file path
	body string // normalized YAML body (only used to enrich error text)
}

// resolveState carries cross-recursion state: the library cache, the
// dedup table, and the include cycle/depth stack.
type resolveState struct {
	cache *libraryCache
	// bound maps "kind/name" → identity of the file that authored it.
	bound map[string]resIdentity
	// stack is the abs-path chain of files currently being resolved,
	// for cycle detection and error messages.
	stack []string
	// prefixes is the nested include-prefix chain, composed by
	// currentPrefix into the effective prefix of the current file.
	prefixes []string
}

// resolveConfig is the single funnel every command passes through. It
// resolves the six resource maps (handling `extern:`), recursively
// merges `include:`d modules, and records ordered route groups for
// later materialization. It does NOT touch routes' $arg/$env or
// prefixing — that is materializeRoutes' sole responsibility.
func resolveConfig(cfg *Config, absPath, baseDir string) error {
	if cfg.Kind != "" {
		return fmt.Errorf("%s is a kind:%s library, not a server — libraries are borrowed via extern:, not booted", absPath, cfg.Kind)
	}
	st := &resolveState{
		cache: newLibraryCache(),
		bound: map[string]resIdentity{},
	}
	if err := resolveModule(cfg, cfg, absPath, baseDir, st); err != nil {
		return err
	}
	return nil
}

// resolveModule resolves one file (host or included) into host: it
// merges resource maps via strict dedup and appends route groups. cfg
// is the file being processed; host is the accumulator everything
// merges into.
func resolveModule(host, cfg *Config, absPath, baseDir string, st *resolveState) error {
	// Cycle + depth check.
	for _, p := range st.stack {
		if p == absPath {
			chain := append(append([]string{}, st.stack...), absPath)
			return fmt.Errorf("include cycle detected: %s (fix: remove the self/mutual include)", strings.Join(chain, " -> "))
		}
	}
	if len(st.stack) >= maxIncludeDepth {
		chain := append(append([]string{}, st.stack...), absPath)
		return fmt.Errorf("include depth exceeds %d: %s (fix: flatten the module hierarchy)", maxIncludeDepth, strings.Join(chain, " -> "))
	}
	st.stack = append(st.stack, absPath)
	defer func() { st.stack = st.stack[:len(st.stack)-1] }()

	// Phase (b): resolve this file's own resource maps from raw nodes.
	if err := resolveResources(host, cfg, absPath, baseDir, st); err != nil {
		return err
	}

	// Phase (c): record this file's authored routes as a route group.
	// The host file is always the first group (empty prefix); each
	// included module appends its own group with the composed prefix
	// applied later by materializeRoutes.
	if rb, err := cfg.RawRoutes.Bytes(); err == nil && len(rb) > 0 && strings.TrimSpace(string(rb)) != "null" {
		host.routeGroups = append(host.routeGroups, routeGroup{
			rawRoutes: cfg.RawRoutes,
			prefix:    currentPrefix(st),
			baseDir:   baseDir,
		})
	}

	// Phase (d): recurse + merge includes.
	for _, inc := range cfg.Include {
		childAbs, err := resolveAbs(baseDir, inc.File)
		if err != nil {
			return fmt.Errorf("%s: include %q: %w", absPath, inc.File, err)
		}
		childRaw, err := os.ReadFile(childAbs)
		if err != nil {
			return fmt.Errorf("%s: include %q: cannot read %s: %w (fix: check the include path)", absPath, inc.File, childAbs, err)
		}
		childExpanded, err := secrets.Expand(string(childRaw))
		if err != nil {
			return fmt.Errorf("%s: include %q: failed to resolve secrets: %w", absPath, inc.File, err)
		}
		var childCfg Config
		if err := yaml.Unmarshal([]byte(childExpanded), &childCfg); err != nil {
			return fmt.Errorf("%s: include %q: failed to parse %s: %w", absPath, inc.File, childAbs, err)
		}
		if childCfg.Kind != "" {
			return fmt.Errorf("%s: include %q points at %s which is a kind:%s library, not a module (fix: borrow libraries via extern:, include only modules/servers)", absPath, inc.File, childAbs, childCfg.Kind)
		}
		if len(childCfg.Args) > 0 || len(childCfg.Env) > 0 {
			return fmt.Errorf("%s: included module %s must not declare `args:`/`env:` (fix: $arg/$env come from the top-level host file only)", absPath, childAbs)
		}
		childDir := filepath.Dir(childAbs)
		// Compose prefixes by pushing this include's prefix onto the
		// stack for the duration of the child recursion.
		st.pushPrefix(inc.Prefix)
		err = resolveModule(host, &childCfg, childAbs, childDir, st)
		st.popPrefix()
		if err != nil {
			return err
		}
	}
	return nil
}

// prefixStack lives alongside the include stack so currentPrefix can
// compose nested include prefixes (/a + /b + /feeds → /a/b/feeds).
func (st *resolveState) pushPrefix(p string) { st.prefixes = append(st.prefixes, p) }
func (st *resolveState) popPrefix()          { st.prefixes = st.prefixes[:len(st.prefixes)-1] }

// resolveResources walks each rawX map on cfg, resolves extern vs
// inline, and binds canonical typed pointers into host with strict
// identity dedup.
func resolveResources(host, cfg *Config, absPath, baseDir string, st *resolveState) error {
	for _, kind := range resourceKinds {
		raw := rawMapFor(cfg, kind)
		for name, node := range raw {
			node := node
			ext, hasExtern, err := externOf(&node)
			if err != nil {
				return fmt.Errorf("%s: %s/%s: %w", absPath, kind, name, err)
			}

			var (
				ownerAbs string
				value    any
			)
			if hasExtern {
				libAbs, err := resolveAbs(baseDir, ext)
				if err != nil {
					return fmt.Errorf("%s: %s/%s: extern %q: %w", absPath, kind, name, ext, err)
				}
				lib, err := st.cache.loadTypedLibrary(libAbs)
				if err != nil {
					return fmt.Errorf("%s: %s/%s: %w", absPath, kind, name, err)
				}
				if lib.kind != kind {
					return fmt.Errorf("%s: %s/%s externs %s which is a kind:%s library (fix: the extern target's kind must match the slot — use a kind:%s library)", absPath, kind, name, libAbs, lib.kind, kind)
				}
				libNode, ok := lib.nodes[name]
				if !ok {
					return fmt.Errorf("%s: %s/%s externs %s but that library has no %q entry (fix: define %q in the library or correct the name)", absPath, kind, name, libAbs, name, name)
				}
				v, err := decodeResource(kind, &libNode)
				if err != nil {
					return fmt.Errorf("%s: %s/%s: decoding library entry from %s: %w", absPath, kind, name, libAbs, err)
				}
				ownerAbs = libAbs
				value = v
			} else {
				v, err := decodeResource(kind, &node)
				if err != nil {
					return fmt.Errorf("%s: %s/%s: %w", absPath, kind, name, err)
				}
				ownerAbs = absPath
				value = v
				// Authored connections owned by THIS module get their
				// inbound subscribe_path prefixed here (the only place
				// authorship + composed prefix are both known). Borrowed
				// (extern) connections keep their canonical path.
				if kind == "connections" {
					if cc, ok := value.(*connections.ConnectionConfig); ok {
						cc.SubscribePath = joinPrefix(currentPrefix(st), cc.SubscribePath)
					}
				}
			}

			key := kind + "/" + name
			id := resIdentity{abs: ownerAbs, body: normalizeNode(&node)}
			if prev, ok := st.bound[key]; ok {
				if prev.abs != ownerAbs {
					return fmt.Errorf("resource %s authored by two files:\n  - %s\n  - %s\nfix: borrow the shared one via extern: instead of re-authoring it", key, prev.abs, ownerAbs)
				}
				// Same identity → idempotent rebind, skip.
				continue
			}
			st.bound[key] = id
			bindResource(host, kind, name, value)
		}
	}
	return nil
}

// externOf inspects a resource node. Returns the extern path (if any)
// and errors if extern is combined with sibling inline keys.
func externOf(n *yaml.Node) (string, bool, error) {
	var probe struct {
		Extern string `yaml:"extern"`
	}
	if err := n.Decode(&probe); err != nil {
		// Non-mapping node (e.g. scalar) — treat as inline, no extern.
		return "", false, nil
	}
	if probe.Extern == "" {
		return "", false, nil
	}
	// Detect sibling keys on the same mapping.
	doc := n
	if doc.Kind == yaml.DocumentNode && len(doc.Content) > 0 {
		doc = doc.Content[0]
	}
	if doc.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(doc.Content); i += 2 {
			if doc.Content[i].Value != "extern" {
				return "", false, fmt.Errorf("extern entry must not also define inline fields (fix: remove the inline keys, or drop extern and author it here)")
			}
		}
	}
	return probe.Extern, true, nil
}

// normalizeNode re-marshals a yaml node with map keys sorted so two
// textually-different but structurally-identical bodies compare equal.
func normalizeNode(n *yaml.Node) string {
	var v any
	if err := n.Decode(&v); err != nil {
		return ""
	}
	b, err := yaml.Marshal(sortAny(v))
	if err != nil {
		return ""
	}
	return string(b)
}

func sortAny(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, vv := range t {
			out[k] = sortAny(vv)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i := range t {
			out[i] = sortAny(t[i])
		}
		return out
	default:
		return v
	}
}

// rawMapFor returns the raw (pre-extern) node map for a kind.
func rawMapFor(cfg *Config, kind string) map[string]yaml.Node {
	switch kind {
	case "storage":
		return cfg.rawStorage
	case "plugins":
		return cfg.rawPlugins
	case "connections":
		return cfg.rawConnections
	case "auth":
		return cfg.rawAuth
	case "requests":
		return cfg.rawRequests
	case "limits":
		return cfg.rawLimits
	}
	return nil
}

// bindResource installs a resolved typed pointer into the host's
// resolved resource map, allocating the map on first use.
func bindResource(host *Config, kind, name string, value any) {
	switch kind {
	case "storage":
		if host.Storage == nil {
			host.Storage = map[string]*storage.StorageConfig{}
		}
		host.Storage[name] = value.(*storage.StorageConfig)
	case "plugins":
		if host.Plugins == nil {
			host.Plugins = map[string]*plugins.PluginConfig{}
		}
		host.Plugins[name] = value.(*plugins.PluginConfig)
	case "connections":
		if host.Connections == nil {
			host.Connections = map[string]*connections.ConnectionConfig{}
		}
		host.Connections[name] = value.(*connections.ConnectionConfig)
	case "auth":
		if host.Auth == nil {
			host.Auth = map[string]*auth.AuthConfig{}
		}
		host.Auth[name] = value.(*auth.AuthConfig)
	case "requests":
		if host.Requests == nil {
			host.Requests = map[string]*httpclient.RequestDef{}
		}
		host.Requests[name] = value.(*httpclient.RequestDef)
	case "limits":
		if host.Limits == nil {
			host.Limits = map[string]*LimitEntry{}
		}
		host.Limits[name] = value.(*LimitEntry)
	}
}

// currentPrefix composes the nested include prefixes on the stack into
// the effective prefix for the file currently being resolved.
func currentPrefix(st *resolveState) string {
	p := ""
	for _, seg := range st.prefixes {
		p = joinPrefix(p, seg)
	}
	return p
}

// joinPrefix joins a route prefix with a path. Empty/"/" prefix → p
// unchanged; otherwise the prefix is normalized to a single leading
// slash, no trailing slash, and concatenated with a single separator.
func joinPrefix(prefix, p string) string {
	if prefix == "" || prefix == "/" {
		return p
	}
	prefix = "/" + strings.Trim(prefix, "/")
	if p == "" || p == "/" {
		return prefix
	}
	return prefix + "/" + strings.TrimLeft(p, "/")
}

// materializeRoutes is the SINGLE place route bytes become []*Route and
// the ONLY place prefixing happens. Idempotent. When args != nil it
// performs the $arg/$env string substitution (mirrors the legacy
// renderVars logic); when args == nil it skips substitution (matches
// existing validate / route_summary behavior, leaving $x literal).
func materializeRoutes(cfg *Config, args map[string]string) error {
	if cfg == nil || cfg.routesMaterialized {
		return nil
	}

	// Fallback for hand-written single-file configs loaded without the
	// resolver (defensive — loadConfig always seeds routeGroups, but a
	// directly-constructed Config in tests may not). If Routes were
	// already populated externally (legacy manual-unmarshal callers)
	// and the resolver never ran, treat them as the materialized set.
	groups := cfg.routeGroups
	if len(groups) == 0 {
		if len(cfg.Routes) > 0 {
			cfg.routesMaterialized = true
			return nil
		}
		if cfg.RawRoutes.Node.Kind != 0 {
			groups = []routeGroup{{rawRoutes: cfg.RawRoutes}}
		}
	}

	for _, g := range groups {
		rb, err := g.rawRoutes.Bytes()
		if err != nil {
			return err
		}
		s := string(rb)
		if strings.TrimSpace(s) == "" || strings.TrimSpace(s) == "null" {
			continue
		}
		if args != nil {
			for key, value := range args {
				s = strings.ReplaceAll(s, fmt.Sprintf("$%s", key), value)
			}
			for key, valueConfig := range cfg.Env {
				value := os.Getenv(key)
				if value == "" && valueConfig != nil && valueConfig.Default != nil && *valueConfig.Default != "" {
					value = *valueConfig.Default
					os.Setenv(key, value)
				}
				if value == "" {
					return fmt.Errorf("missing env value for: %s", key)
				}
				s = strings.ReplaceAll(s, fmt.Sprintf("$%s", key), value)
			}
		}
		var routes []*Route
		if err := yaml.Unmarshal([]byte(s), &routes); err != nil {
			return err
		}
		applyPrefix(g.prefix, routes)
		cfg.Routes = append(cfg.Routes, routes...)
	}

	cfg.routesMaterialized = true
	return nil
}

// applyPrefix rewrites the locked inbound path-shaped fields of a
// module's authored routes. A route with `absolute: true` is skipped
// entirely. Only values starting with "/" are rewritten; absolute
// http(s):// URLs and the five outbound URL fields are never touched.
func applyPrefix(prefix string, routes []*Route) {
	if prefix == "" || prefix == "/" {
		return
	}
	for _, r := range routes {
		if r == nil || r.Absolute {
			continue
		}
		r.Path = prefixPath(prefix, r.Path)

		if r.AuthLoginConfig != nil {
			r.AuthLoginConfig.RedirectOnSuccess = prefixPath(prefix, r.AuthLoginConfig.RedirectOnSuccess)
			r.AuthLoginConfig.RedirectOnFailure = prefixPath(prefix, r.AuthLoginConfig.RedirectOnFailure)
		}
		if r.AuthSignupConfig != nil {
			r.AuthSignupConfig.RedirectOnSuccess = prefixPath(prefix, r.AuthSignupConfig.RedirectOnSuccess)
			r.AuthSignupConfig.RedirectOnFailure = prefixPath(prefix, r.AuthSignupConfig.RedirectOnFailure)
		}
		if r.AuthLogoutConfig != nil {
			r.AuthLogoutConfig.RedirectOnSuccess = prefixPath(prefix, r.AuthLogoutConfig.RedirectOnSuccess)
			r.AuthLogoutConfig.RedirectOnFailure = prefixPath(prefix, r.AuthLogoutConfig.RedirectOnFailure)
		}
		if r.MagicLinkRequestConfig != nil {
			r.MagicLinkRequestConfig.CallbackURL = prefixPath(prefix, r.MagicLinkRequestConfig.CallbackURL)
		}
		if r.MagicLinkConsumeConfig != nil {
			r.MagicLinkConsumeConfig.SuccessRedirect = prefixPath(prefix, r.MagicLinkConsumeConfig.SuccessRedirect)
		}
	}
}

// prefixPath applies the prefix only to a local ("/"-rooted) path.
// Empty values and absolute http(s):// URLs pass through untouched.
func prefixPath(prefix, p string) string {
	if p == "" {
		return p
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p
	}
	if !strings.HasPrefix(p, "/") {
		return p
	}
	return joinPrefix(prefix, p)
}
