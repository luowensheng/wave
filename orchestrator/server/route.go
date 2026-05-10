package servers

import (
	"wave/infra/inputs"
	"wave/infra/render"
	"wave/usecases/routes"
	"fmt"
	"net/http"
)

type  Route struct {
	Path    string   `yaml:"path,omitempty" json:"path,omitempty"`
	Method  string   `yaml:"method,omitempty" json:"method,omitempty"`
	Methods []string `yaml:"methods,omitempty" json:"methods,omitempty"`

	Script      string `yaml:"script,omitempty" json:"script,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Type        string `yaml:"type,omitempty" json:"type,omitempty"`

	ValidateCSRF bool `yaml:"validate_csrf,omitempty" json:"validate_csrf,omitempty"`
	IncludeCSRF  bool `yaml:"include_csrf,omitempty" json:"include_csrf,omitempty"`

	Auth []string `yaml:"auth,omitempty" json:"auth,omitempty"`

	StaticDirConfig     *routes.StaticConfig        `yaml:"static,omitempty" json:"static,omitempty"`
	FileConfig          *routes.FileConfig          `yaml:"file,omitempty" json:"file,omitempty"`
	ForwardConfig       *routes.ForwardConfig       `yaml:"forward,omitempty" json:"forward,omitempty"`
	RedirectConfig      *routes.RedirectConfig      `yaml:"redirect,omitempty" json:"redirect,omitempty"`
	APIConfig           *routes.APIConfig           `yaml:"api,omitempty" json:"api,omitempty"`
	ContentConfig       *routes.ContentConfig       `yaml:"content,omitempty" json:"content,omitempty"`
	AuthLoginConfig     *routes.AuthLoginConfig     `yaml:"auth-login,omitempty" json:"auth_login,omitempty"`
	AuthSignupConfig    *routes.AuthSignupConfig    `yaml:"auth-signup,omitempty" json:"auth_signup,omitempty"`
	AuthLogoutConfig    *routes.AuthLogoutConfig    `yaml:"auth-logout,omitempty" json:"auth_logout,omitempty"`
	StorageAccessConfig *routes.StorageAccessConfig `yaml:"storage-access,omitempty" json:"storage_access,omitempty"`
	DependenciesConfig  *routes.DependenciesConfig  `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
	ProcessConfig       *routes.ProcessConfig       `yaml:"process,omitempty" json:"process,omitempty"`
	FileServerConfig    *routes.FileServerConfig    `yaml:"file-server,omitempty" json:"file_server,omitempty"`
	PluginConfig        *routes.PluginConfig        `yaml:"plugin,omitempty" json:"plugin,omitempty"`
	StreamPublishConfig *routes.StreamPublishConfig `yaml:"stream-publish,omitempty" json:"stream_publish,omitempty"`
	GraphQLConfig       *routes.GraphQLConfig       `yaml:"graphql,omitempty" json:"graphql,omitempty"`

	MagicLinkRequestConfig  *routes.MagicLinkRequestConfig  `yaml:"magic-link-request,omitempty" json:"magic_link_request,omitempty"`
	MagicLinkConsumeConfig  *routes.MagicLinkConsumeConfig  `yaml:"magic-link-consume,omitempty" json:"magic_link_consume,omitempty"`
	TOTPEnrollStartConfig   *routes.TOTPEnrollStartConfig   `yaml:"totp-enroll-start,omitempty" json:"totp_enroll_start,omitempty"`
	TOTPEnrollConfirmConfig *routes.TOTPEnrollConfirmConfig `yaml:"totp-enroll-confirm,omitempty" json:"totp_enroll_confirm,omitempty"`
	TOTPVerifyConfig        *routes.TOTPVerifyConfig        `yaml:"totp-verify,omitempty" json:"totp_verify,omitempty"`

	OAuthStartConfig    *routes.OAuthStartConfig    `yaml:"oauth-start,omitempty" json:"oauth_start,omitempty"`
	OAuthCallbackConfig *routes.OAuthCallbackConfig `yaml:"oauth-callback,omitempty" json:"oauth_callback,omitempty"`

	config    RouteConfig // intentionally untagged
	Whitelist []string    `yaml:"ip_whitelist,omitempty" json:"ip_whitelist,omitempty"`
	Blacklist []string    `yaml:"ip_blacklist,omitempty" json:"ip_blacklist,omitempty"`

	// Per-route CORS allowlist. Adds Access-Control-Allow-Origin and
	// answers OPTIONS preflights when set.
	CorsOrigins []string `yaml:"cors_origins,omitempty" json:"cors_origins,omitempty"`

	// Webhook signature verification (Stripe / GitHub / Slack / generic).
	// When set, requests missing or failing the signature get 401 before
	// reaching the route handler. Designed for stream-publish routes
	// receiving webhooks from third parties.
	WebhookSig *WebhookSigConfig `yaml:"webhook_sig,omitempty" json:"webhook_sig,omitempty"`

	// ForwardAuth delegates request auth to an external service. Wraps
	// the handler — on auth-service non-2xx, the route never runs.
	ForwardAuth *ForwardAuthConfig `yaml:"forward_auth,omitempty" json:"forward_auth,omitempty"`

	// Limits is a list of names that resolve against the top-level
	// `Config.Limits` registry at boot. Each named entry covers
	// exactly one case (body_too_large, rate_limited, etc.).
	//
	// Composition by listing: a route declares
	//
	//	limits: [size_5mb, rate_100, json_inputs]
	//
	// to pull in three independent failure handlers. When two
	// referenced names happen to bind the same Case, the LATER one in
	// the list wins (configuration-cascade override).
	//
	// No inline definitions are allowed here — every name in this
	// list MUST be predefined at the top level.
	Limits []string `yaml:"limits,omitempty" json:"limits,omitempty"`

	// resolvedLimits is the case→entry map populated at boot from the
	// names in Limits. Untagged so it never round-trips through YAML.
	resolvedLimits map[string]*LimitEntry

	// Inputs declares the named, typed values this route accepts. Each
	// entry pulls one value from a configurable source (path/query/
	// body/form/header/cookie), coerces it to a type, runs validators,
	// and stuffs the result on the request context.
	//
	// On any validation failure the request gets a single 400 listing
	// every problem at once.
	//
	// Routes that pass requests through to multiple downstreams unchanged
	// (`type: plugin`, `type: forward`, `type: dynamic_forward`) are
	// EXEMPT from the strict-scope rule — declaring inputs is still
	// allowed (and useful for OpenAPI docs), but unknown body fields
	// are not stripped.
	Inputs []inputs.Spec `yaml:"inputs,omitempty" json:"inputs,omitempty"`

	// inputsSet is the compiled form populated by setRouteConfig().
	// Not directly serializable.
	inputsSet *inputs.SpecSet

	// Per-route response cache. GET-only; never caches non-2xx or
	// responses that set Cache-Control: no-store. Optional `key_by_auth`
	// scopes the cache per Authorization header so authenticated users
	// don't see each other's results.
	Cache *RouteCacheConfig `yaml:"cache,omitempty" json:"cache,omitempty"`

	// Request body validation. When set, JSON request bodies are
	// validated against the schema and rejected with 400 + a list of
	// problems before the handler runs. Subset of JSON Schema —
	// see infra/jsonschema for what's supported.
	RequestSchema *RouteSchemaConfig `yaml:"request_schema,omitempty" json:"request_schema,omitempty"`

	// Claims-based RBAC. RequireRoles + RequireClaims are AND-ed: a
	// request must satisfy *all* listed conditions. Roles are looked up
	// in the OIDC ID-token's "roles" or "groups" claim. RequireClaims
	// matches exact key=value pairs ("plan": "enterprise").
	RequireRoles  []string          `yaml:"require_roles,omitempty" json:"require_roles,omitempty"`
	RequireClaims map[string]string `yaml:"require_claims,omitempty" json:"require_claims,omitempty"`
}

// RouteCacheConfig is the YAML shape for per-route response caching.
type RouteCacheConfig struct {
	TTL        string `yaml:"ttl,omitempty" json:"ttl,omitempty"`               // e.g. "30s", "5m" — default 1m
	MaxEntries int    `yaml:"max_entries,omitempty" json:"max_entries,omitempty"` // default 1024
	KeyByAuth  bool   `yaml:"key_by_auth,omitempty" json:"key_by_auth,omitempty"` // scope per Authorization
}

// RouteSchemaConfig points the request body validator at a JSON
// Schema. Either Inline (literal subset-schema) or Path (path to a
// .json file relative to the config dir) — Inline wins if both set.
type RouteSchemaConfig struct {
	Inline map[string]any `yaml:"inline,omitempty" json:"inline,omitempty"`
	Path   string         `yaml:"path,omitempty" json:"path,omitempty"`
}

// ForwardAuthConfig delegates request authorization to an external
// auth service (Authelia, Authentik, oauth2-proxy, etc.). On 2xx the
// request continues; on anything else the auth service's response is
// mirrored to the client (so OAuth-style redirects propagate).
type ForwardAuthConfig struct {
	URL               string   `yaml:"url,omitempty" json:"url,omitempty"`
	Method            string   `yaml:"method,omitempty" json:"method,omitempty"`
	TimeoutSec        int      `yaml:"timeout_sec,omitempty" json:"timeout_sec,omitempty"`
	ForwardHeaders    []string `yaml:"forward_headers,omitempty" json:"forward_headers,omitempty"`
	ResponseHeaders   []string `yaml:"response_headers,omitempty" json:"response_headers,omitempty"`
	TrustForwardedFor bool     `yaml:"trust_forwarded_for,omitempty" json:"trust_forwarded_for,omitempty"`
}

// WebhookSigConfig is the YAML shape for per-route webhook HMAC auth.
type WebhookSigConfig struct {
	Provider     string `yaml:"provider,omitempty" json:"provider,omitempty"`         // stripe|github|slack|generic
	Secret       string `yaml:"secret,omitempty" json:"secret,omitempty"`             // shared secret (use ${ENV:X})
	Header       string `yaml:"header,omitempty" json:"header,omitempty"`             // override default header
	Algorithm    string `yaml:"algorithm,omitempty" json:"algorithm,omitempty"`       // generic only: sha256|sha1
	HeaderPrefix string `yaml:"header_prefix,omitempty" json:"header_prefix,omitempty"` // generic only: e.g. "sha256="
	ToleranceSec int    `yaml:"tolerance_sec,omitempty" json:"tolerance_sec,omitempty"` // 0 → 5min default
}

func (r *Route) Validate() error {
	if r.Path == "" {
		return fmt.Errorf("missing path")
	}
	return nil
}
func (r *Route) render(data map[string]string) error {
	var err error
	// args := map[string]any{"args": data}

	// r.FilePath, err = RenderToString(r.FilePath, args)
	// if err != nil {
	// 	return err
	// }

	r.Path, err = render.RenderToString(r.Path, data)
	if err != nil {
		return err
	}

	// r.Dir, err = RenderToString(r.Dir, args)
	// if err != nil {
	// 	return err
	// }

	// r.ForwardURL, err = RenderToString(r.ForwardURL, args)
	// if err != nil {
	// 	return err
	// }

	return nil
}

type RouteConfig interface {
	// Render(data map[string]string) error
	CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error)
	// Validate() error
}

func (route *Route) setRouteConfig() error {
	config, err := route.getRouteConfig()
	if err != nil {
		return err
	}
	if config == nil {
		return fmt.Errorf("missing config fields fpr : '%s'", route.Path)
	}
	route.config = config

	// Compile declared inputs once at boot so per-request validation
	// is just map lookups + coercion. Empty list = no-op middleware.
	if len(route.Inputs) > 0 {
		set, err := inputs.Compile(route.Inputs)
		if err != nil {
			return fmt.Errorf("inputs for path=%q: %w", route.Path, err)
		}
		route.inputsSet = set
	}
	return nil
}

// InputsSet returns the compiled input spec for this route, or nil
// when none was declared. Used by api / storage_access to enforce
// strict template scope.
func (route *Route) InputsSet() *inputs.SpecSet { return route.inputsSet }

func (route *Route) getRouteConfig() (RouteConfig, error) {
	var routeConfig RouteConfig
	switch route.Type {
	case "static":
		routeConfig = route.StaticDirConfig
		// s.setupStaticRoute(route)
		// StaticDirs = append(StaticDirs, route.Dir)
	case "file":
		routeConfig = route.FileConfig
		// s.setupFileRoute(route)
	case "forward":
		routeConfig = route.ForwardConfig
		// s.setupForwardRoute(route)
	case "api":
		routeConfig = route.APIConfig
		// s.setupAPIRoute(route)
	case "content":
		routeConfig = route.ContentConfig
		// s.setupContentRoute(route)
	case "auth-login":
		routeConfig = route.AuthLoginConfig
		// s.setupAuthLoginRoute(route)
	case "auth-signup":
		routeConfig = route.AuthSignupConfig
		// s.setupAuthSignupRoute(route)
	case "auth-logout":
		routeConfig = route.AuthLogoutConfig
		// s.setupAuthLogoutRoute(route)
	case "storage-access":
		routeConfig = route.StorageAccessConfig
		// s.setupAccessStorageRoute(route)
	case "dependencies":
		routeConfig = route.DependenciesConfig
		// s.setupDependencyRoute(route)
	case "process":
		// `type: process` runs a shell/script per request and streams its
		// stdout to the response. Useful for shell-based "instant APIs":
		// pipe through jq, sed, awk, curl, etc. without writing a Go
		// or Python program. Distinct from `type: plugin` (which is a
		// JSON-in/JSON-out RPC contract) — keep both.
		routeConfig = route.ProcessConfig
		// s.setupProcessRoute(route)
	case "file-server": //, "file_server":
		routeConfig = route.FileServerConfig
		// s.setupFileServerRoute(route)
	case "plugin":
		routeConfig = route.PluginConfig
	case "stream-publish":
		routeConfig = route.StreamPublishConfig
	case "graphql":
		routeConfig = route.GraphQLConfig
	case "magic-link-request":
		routeConfig = route.MagicLinkRequestConfig
	case "magic-link-consume":
		routeConfig = route.MagicLinkConsumeConfig
	case "totp-enroll-start":
		routeConfig = route.TOTPEnrollStartConfig
	case "totp-enroll-confirm":
		routeConfig = route.TOTPEnrollConfirmConfig
	case "totp-verify":
		routeConfig = route.TOTPVerifyConfig
	case "oauth-start":
		routeConfig = route.OAuthStartConfig
	case "oauth-callback":
		routeConfig = route.OAuthCallbackConfig
	default:
		// log.Fatalf("Unknown route type: %s", route.Type)
		return nil, fmt.Errorf("unknown route type: %s", route.Type)
	}

	if routeConfig == nil {
		return nil, fmt.Errorf("missong config field '%s' for path='%s'", route.Type, route.Path)
	}

	return routeConfig, nil
}
