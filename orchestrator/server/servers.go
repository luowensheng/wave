package servers

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"wave/orchestrator/features/auth"
	"wave/infra/audit"
	"wave/infra/bundler"
	"wave/infra/connections"
	"wave/infra/errreport"
	"wave/infra/forwardauth"
	infrahttp "wave/infra/http"
	"wave/infra/httpclient"
	"wave/infra/inputs"
	"wave/infra/ipfilter"
	"wave/infra/net"
	"wave/infra/observability"
	"wave/infra/common"
	"wave/infra/plugins"
	"wave/infra/rbac"
	"wave/infra/secrets"
	"wave/infra/webhooksig"
	"wave/usecases/routes"
	"wave/usecases/schedule"
	storageaccess "wave/usecases/storage_access"
	taskroute "wave/usecases/task"
	"wave/orchestrator/features/storage"
	orchusecases "wave/orchestrator/usecases"

	"log"

	"gopkg.in/yaml.v3"
)

type Defaults struct {
	Port                *int    `yaml:"port"`
	Host                *string `yaml:"host"`
	ExpectedContentType string  `yaml:"expected_content_type,omitempty" json:"expected_content_type,omitempty"`
}

type HTTPSConfig struct {
	SSLKeyfile   string   `json:"ssl_keyfile" yaml:"ssl_keyfile"`
	SSLCertfile  string   `json:"ssl_certfile" yaml:"ssl_certfile"`
	Generate     bool     `json:"generate" yaml:"generate"`
	Organization []string `json:"organization" yaml:"organization"`
	DNSNames     []string `json:"dns_names" yaml:"dns_names"`

	CommonName string `json:"common_name" yaml:"common_name"`
}

type Arg struct {
	Default     *string `yaml:"default" json:"default"`
	Description string  `yaml:"description" json:"description"`
}

// IncludeRef is a single composition reference declared under the
// host/module `include:` list. File is a path relative to the
// declaring file's directory; Prefix (optional) shifts every
// inbound path-shaped field of the included module's authored routes.
type IncludeRef struct {
	File   string `yaml:"file" json:"file"`
	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`
}

// routeGroup is one ordered slab of raw route YAML to materialize.
// The host file is always the first group (empty prefix, host
// baseDir); each recursively-included module appends one group with
// its effective composed prefix and its own baseDir.
type routeGroup struct {
	rawRoutes common.RawYAML
	prefix    string
	baseDir   string
}

type Config struct {
	JSONDiscoveryRoutePath string `yaml:"json_discovery_route_path" json:"json_discovery_route_path"`
	HTMLDiscoveryRoutePath string `yaml:"html_discovery_route_path" json:"html_discovery_route_path"`

	Defaults *Defaults `yaml:"default" json:"default"`

	// Resource maps are resolved (post-composition) form. They carry
	// `yaml:"-"` because raw YAML is decoded into the sibling private
	// `rawX map[string]yaml.Node` fields first; the resolver then walks
	// those nodes (handling `extern:`) and populates these typed maps.
	// All downstream consumers (InitDependencies, validators, etc.)
	// read these typed maps unchanged.
	Storage     map[string]*storage.StorageConfig        `yaml:"-" json:"storage,omitempty"`
	HTTPSConfig *HTTPSConfig                             `yaml:"https_config,omitempty" json:"https_config,omitempty"`
	Args        map[string]*Arg                          `yaml:"args,omitempty" json:"args,omitempty"`
	Env         map[string]*Arg                          `yaml:"env,omitempty" json:"env,omitempty"`

	Auth        map[string]*auth.AuthConfig              `yaml:"-" json:"auth,omitempty"`
	Build       *bundler.Config                          `yaml:"build,omitempty" json:"build,omitempty"`
	Plugins     map[string]*plugins.PluginConfig         `yaml:"-" json:"plugins,omitempty"`
	Connections map[string]*connections.ConnectionConfig `yaml:"-" json:"connections,omitempty"`
	RawRoutes   common.RawYAML                           `yaml:"routes,omitempty" json:"-,omitempty"`
	Routes      []*Route                                 `yaml:"-" json:"Routes"`

	// Raw (pre-extern) resource nodes. Populated by Config.UnmarshalYAML
	// (yaml.v3 cannot fill unexported fields, so they are captured
	// manually there) and consumed once by the resolver. Never read
	// after resolution.
	rawStorage     map[string]yaml.Node `yaml:"-" json:"-"`
	rawAuth        map[string]yaml.Node `yaml:"-" json:"-"`
	rawPlugins     map[string]yaml.Node `yaml:"-" json:"-"`
	rawConnections map[string]yaml.Node `yaml:"-" json:"-"`
	rawRequests    map[string]yaml.Node `yaml:"-" json:"-"`
	rawLimits      map[string]yaml.Node `yaml:"-" json:"-"`

	// Kind, when non-empty, marks this file as a typed resource library
	// (not a server). Booting a library is an explicit error.
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`

	// Include lists module/host composition references resolved by the
	// resolver. Each entry merges another file's resources and routes,
	// the latter optionally shifted under Prefix.
	Include []IncludeRef `yaml:"include,omitempty" json:"include,omitempty"`

	// routeGroups holds, in include order, the raw route YAML for the
	// host plus every recursively-included module together with the
	// effective prefix and the module's baseDir. materializeRoutes is
	// the SOLE consumer; it is the ONLY place prefixing happens.
	routeGroups []routeGroup

	// routesMaterialized guards the lazy route materialization done by
	// renderVars / validate / route_summary so they converge on the
	// merged+prefixed route set.
	routesMaterialized bool

	IpFilter *struct {
		Whitelist []string `yaml:"ip_whitelist,omitempty" json:"ip_whitelist,omitempty"`
		Blacklist []string `yaml:"ip_blacklist,omitempty" json:"ip_blacklist,omitempty"`
	} `yaml:"ip_filter,omitempty" json:"ip_filter,omitempty"`

	// OutboxDB enables the durable outbound webhook outbox. SQLite path
	// (created if missing); a background worker drains it. Empty disables.
	OutboxDB string `yaml:"outbox_db,omitempty" json:"outbox_db,omitempty"`

	// Schedule lists in-process scheduled jobs keyed by name. Each job
	// invokes a configured plugin on a fixed interval (`every: 30s`) or
	// daily at a wall-clock time (`at: "07:30"`). Not persisted across
	// restarts. The map key is the job name — consistent with plugins,
	// connections, storage, and auth top-level blocks.
	Schedule map[string]*ScheduledJob `yaml:"schedule,omitempty" json:"schedule,omitempty"`

	// Requests is a registry of named outbound HTTP request definitions.
	// Referenced by `action.ref` in schedule jobs and type:fetch routes.
	// Resolved form — see rawRequests / the resolver.
	Requests map[string]*httpclient.RequestDef `yaml:"-" json:"requests,omitempty"`

	// AuthFlows configures email/SMS senders for magic-link login,
	// email verification, password reset, and TOTP. Optional — if
	// unset, console senders log to stderr (dev-friendly).
	AuthFlows *AuthFlowsConfig `yaml:"auth_flows,omitempty" json:"auth_flows,omitempty"`

	// NotFound is a catch-all Route definition used when no other route
	// matches the request URL. Any existing route type works — file,
	// content, plugin, forward, etc. The handler runs with HTTP 404
	// status by default; the underlying route type can override that
	// (e.g. a plugin may decide to return 200 with a help page).
	NotFound *Route `yaml:"not_found,omitempty" json:"not_found,omitempty"`

	// Limits is a registry of named LimitEntry definitions. Routes
	// reference entries by name from this map via Route.Limits []string.
	// Each entry covers exactly one Case (body_too_large,
	// rate_limited, etc.). Bundles are expressed by listing several
	// names on a route. Mirrors the pattern used by Auth, Plugins,
	// Connections, Storage. Resolved form — see rawLimits / the resolver.
	Limits map[string]*LimitEntry `yaml:"-" json:"limits,omitempty"`

	// Observability selects which exporter-kind plugins receive the
	// fan-out push of metrics / traces / logs. Empty list = Prometheus
	// scrape endpoint only (back-compat).
	Observability *ObservabilityConfig `yaml:"observability,omitempty" json:"observability,omitempty"`
}

// AuthFlowsConfig holds wiring for the email/SMS-based auth flows.
type AuthFlowsConfig struct {
	// SMTP sender (mailer). Empty Host falls back to console.
	SMTP struct {
		Host     string `yaml:"host,omitempty" json:"host,omitempty"`
		Port     int    `yaml:"port,omitempty" json:"port,omitempty"`
		Username string `yaml:"username,omitempty" json:"username,omitempty"`
		Password string `yaml:"password,omitempty" json:"password,omitempty"`
		From     string `yaml:"from,omitempty" json:"from,omitempty"`
		UseTLS   bool   `yaml:"use_tls,omitempty" json:"use_tls,omitempty"`
	} `yaml:"smtp,omitempty" json:"smtp,omitempty"`

	// Twilio sender (sms). Empty AccountSID falls back to console.
	Twilio struct {
		AccountSID string `yaml:"account_sid,omitempty" json:"account_sid,omitempty"`
		AuthToken  string `yaml:"auth_token,omitempty" json:"auth_token,omitempty"`
		From       string `yaml:"from,omitempty" json:"from,omitempty"`
	} `yaml:"twilio,omitempty" json:"twilio,omitempty"`

	// VerifyHMACSecret is the persistent key used to hash tokens at
	// rest. Survive restarts → live tokens stay valid. Empty → random
	// (dev mode).
	VerifyHMACSecret string `yaml:"verify_hmac_secret,omitempty" json:"verify_hmac_secret,omitempty"`

	// VerifyDB is an optional SQLite path for persisted tokens. Empty
	// → in-memory store (lost on restart, fine for dev).
	VerifyDB string `yaml:"verify_db,omitempty" json:"verify_db,omitempty"`
}

// ScheduledAction and ScheduledSink are aliases to the canonical types
// in usecases/schedule. The schedule package owns the YAML schema for
// actions and sinks (api | storage | plugin | publish | for_each), the
// validation rules, and the executor. Keeping these as aliases means
// any new sink/action type added in the schedule package becomes
// immediately usable from the top-level `schedule:` YAML block with
// no re-wiring here.
type ScheduledAction = schedule.Action
type ScheduledSink = schedule.Sink

// ScheduledJob is one entry under the top-level `schedule:` block.
// The name is the map key in Config.Schedule — it is not a struct field.
type ScheduledJob struct {
	Plugin     string           `yaml:"plugin,omitempty" json:"plugin,omitempty"`
	TriggerKey string           `yaml:"trigger_key,omitempty" json:"trigger_key,omitempty"`
	Every      string           `yaml:"every,omitempty" json:"every,omitempty"`
	At         string           `yaml:"at,omitempty" json:"at,omitempty"`
	Body       map[string]any   `yaml:"body,omitempty" json:"body,omitempty"`
	Action     *ScheduledAction `yaml:"action,omitempty" json:"action,omitempty"`
	Then       []*ScheduledSink `yaml:"then,omitempty" json:"then,omitempty"`
}

// Server struct
type Server struct {
	Config  *Config
	mux     *http.ServeMux
	Address string
	Args    map[string]string

	// storageRefs map[string]storage.StorageRef
	Debug bool

	// fanout owns plugin exporter goroutines; set during Start so Stop
	// can drain in-flight batches at graceful-shutdown time.
	fanout *observability.Fanout
}

func (s *Server) HandleFunc(route *Route) error {
	pattern := strings.TrimSpace(fmt.Sprintf("%s %s", route.Method, route.Path))
	log.Printf("registering route: pattern=%q auth=%v csrfValidate=%v csrfInclude=%v",
		pattern, route.Auth, route.ValidateCSRF, route.IncludeCSRF)

	handler, err := route.config.CreateRoute(route.Method, route.Path, s.Args)
	if err != nil {
		log.Printf("route creation failed: pattern=%q err=%v", pattern, err)
		return err
	}

	allowedMethods := []string{}
	route.Methods = append(route.Methods, route.Method)
	for _, method := range route.Methods {
		m := strings.ToUpper(strings.TrimSpace(method))
		if m == "" {
			continue
		}
		allowedMethods = append(allowedMethods, m)
	}

	var wrappedHandler http.HandlerFunc

	if len(allowedMethods) > 0 {
		log.Printf("route=%q allowedMethods=%v", pattern, allowedMethods)
		wrappedHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			log.Printf("incoming request: %s %s", r.Method, r.URL.Path)
			if !slices.Contains(allowedMethods, r.Method) {
				log.Printf("method not allowed: route=%q method=%s allowed=%v", pattern, r.Method, allowedMethods)
				http.Error(w, fmt.Sprintf("method not allowed: %s", r.Method), http.StatusMethodNotAllowed)
				return
			}
			handler(w, r)
		})
	} else {
		log.Printf("route=%q has no allowedMethods; using raw handler", pattern)
		wrappedHandler = http.HandlerFunc(handler)
	}

	if route.ValidateCSRF {
		log.Printf("route=%q applying CSRF validation middleware", pattern)
		wrappedHandler = s.csrfMiddleware(wrappedHandler)
	} else if route.IncludeCSRF {
		log.Printf("route=%q applying CSRF inclusion middleware", pattern)
		prev := wrappedHandler
		wrappedHandler = func(w http.ResponseWriter, r *http.Request) {
			s.IncludeCSRFToken(w, r)
			prev(w, r)
		}
	}

	// RBAC must wrap *inside* auth (so claims are present), so apply it
	// to the unwrapped handler before the auth middleware goes on.
	if len(route.RequireRoles) > 0 || len(route.RequireClaims) > 0 {
		log.Printf("route=%q applying rbac roles=%v claims=%v",
			pattern, route.RequireRoles, route.RequireClaims)
		policy := rbac.Policy{Roles: route.RequireRoles, Claims: route.RequireClaims}
		base := rbac.Middleware(policy)(wrappedHandler).ServeHTTP
		// limits[case=forbidden] swaps the 403 for the configured action.
		if _, onFail := s.limitFor(route, CaseForbidden); onFail != nil {
			base = swapStatus(base, http.StatusForbidden, onFail).ServeHTTP
		}
		wrappedHandler = base
	}

	if len(route.Auth) > 0 {
		log.Printf("route=%q applying auth middleware roles=%v", pattern, route.Auth)
		base := auth.RequireAuth(wrappedHandler, route.Auth...).ServeHTTP
		// limits[case=unauthenticated] swaps the 401 (or 302 to login)
		// for the configured action.
		if _, onFail := s.limitFor(route, CaseUnauthenticated); onFail != nil {
			base = swapStatus(base, http.StatusUnauthorized, onFail).ServeHTTP
		}
		wrappedHandler = base
	}

	if len(route.Whitelist) > 0 || len(route.Blacklist) > 0 {

		log.Printf("route=%q applying IP filters: whitelist=%v blacklist=%v",
			pattern, route.Whitelist, route.Blacklist)

		filter := ipfilter.NewIPFilterCombined(route.Whitelist, route.Blacklist)

		wrappedHandler = filter.MiddlewareFunc(wrappedHandler)
	}

	if route.RequestSchema != nil {
		mw, err := schemaMiddleware(route.RequestSchema)
		if err != nil {
			return fmt.Errorf("route=%q request_schema: %w", pattern, err)
		}
		if mw != nil {
			log.Printf("route=%q applying request_schema validation", pattern)
			wrappedHandler = mw(wrappedHandler).ServeHTTP
		}
	}

	// Declared-input parsing + validation. Runs after auth (so probes
	// can't enumerate the shape unauthenticated) but before the inner
	// handler — which can then read inputs.FromContext(ctx) instead of
	// re-parsing the request.
	if set := route.InputsSet(); set != nil {
		_, onFail := s.limitFor(route, CaseInvalidInputs)
		log.Printf("route=%q declared-inputs count=%d onFail=%v", pattern, len(set.List), onFail != nil)
		mw := inputs.MiddlewareWithFail(set, onFail)
		wrappedHandler = mw(wrappedHandler).ServeHTTP
	}

	if route.ForwardAuth != nil && route.ForwardAuth.URL != "" {
		v, err := forwardauth.New(forwardauth.Config{
			URL: route.ForwardAuth.URL, Method: route.ForwardAuth.Method,
			Timeout:           time.Duration(route.ForwardAuth.TimeoutSec) * time.Second,
			ForwardHeaders:    route.ForwardAuth.ForwardHeaders,
			ResponseHeaders:   route.ForwardAuth.ResponseHeaders,
			TrustForwardedFor: route.ForwardAuth.TrustForwardedFor,
		})
		if err != nil {
			return fmt.Errorf("route=%q forward_auth: %w", pattern, err)
		}
		log.Printf("route=%q forward_auth url=%s", pattern, route.ForwardAuth.URL)
		mw := v.Middleware
		wrappedHandler = mw(wrappedHandler).ServeHTTP
	}

	if route.WebhookSig != nil && route.WebhookSig.Provider != "" {
		v, err := webhooksig.New(webhooksig.Config{
			Provider:     route.WebhookSig.Provider,
			Secret:       route.WebhookSig.Secret,
			Header:       route.WebhookSig.Header,
			Algorithm:    route.WebhookSig.Algorithm,
			HeaderPrefix: route.WebhookSig.HeaderPrefix,
			Tolerance:    time.Duration(route.WebhookSig.ToleranceSec) * time.Second,
		})
		if err != nil {
			return fmt.Errorf("route=%q webhook_sig: %w", pattern, err)
		}
		log.Printf("route=%q webhook_sig provider=%s", pattern, route.WebhookSig.Provider)
		provider := route.WebhookSig.Provider
		routePattern := pattern
		_, sigOnFail := s.limitFor(route, CaseMissingSignature)
		next := wrappedHandler
		wrappedHandler = func(w http.ResponseWriter, r *http.Request) {
			if err := v.Verify(r); err != nil {
				audit.Emit(audit.Event{
					Action: "webhook.verify", Outcome: "failure",
					Target: routePattern, IP: infrahttp.ClientIP(r),
					RequestID: infrahttp.RequestIDFrom(r.Context()),
					Error:     err.Error(),
					Meta:      map[string]any{"provider": provider},
				})
				if sigOnFail != nil {
					sigOnFail(w, r)
					return
				}
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
			audit.Emit(audit.Event{
				Action: "webhook.verify", Outcome: "success",
				Target: routePattern, IP: infrahttp.ClientIP(r),
				RequestID: infrahttp.RequestIDFrom(r.Context()),
				Meta:      map[string]any{"provider": provider},
			})
			next(w, r)
		}
	}

	// Circuit breaker — driven solely by limits[case=circuit_open].
	if entry, onFail := s.limitFor(route, CaseCircuitOpen); entry != nil {
		cooldown, _ := time.ParseDuration(entry.Cooldown)
		cb := infrahttp.NewCircuitBreaker(entry.FailureThreshold, cooldown)
		log.Printf("route=%q circuit threshold=%d cooldown=%v onFail=%v",
			pattern, entry.FailureThreshold, cooldown, onFail != nil)
		wrappedHandler = cb.MiddlewareWithFail(wrappedHandler, onFail).ServeHTTP
	}

	if route.Cache != nil {
		ttl, _ := time.ParseDuration(route.Cache.TTL)
		cache := infrahttp.NewResponseCache(route.Cache.MaxEntries, ttl, route.Cache.KeyByAuth)
		log.Printf("route=%q cache ttl=%v max=%d key_by_auth=%v",
			pattern, ttl, route.Cache.MaxEntries, route.Cache.KeyByAuth)
		mw := cache.Middleware
		wrappedHandler = mw(wrappedHandler).ServeHTTP
	}

	// Rate limiting — driven solely by limits[case=rate_limited].
	var (
		rps        float64
		burst      float64
		keyClaim   string
		rateOnFail http.HandlerFunc
	)
	if entry, onFail := s.limitFor(route, CaseRateLimited); entry != nil {
		rps = entry.RPS
		burst = entry.Burst
		keyClaim = entry.KeyClaim
		rateOnFail = onFail
	}
	if rps > 0 {
		if burst <= 0 {
			burst = rps
		}
		log.Printf("route=%q rate-limit rps=%.1f burst=%.1f key_claim=%q onFail=%v",
			pattern, rps, burst, keyClaim, rateOnFail != nil)
		tb := infrahttp.NewTokenBucket(rps, burst)

		keyFn := infrahttp.ClientIP
		if claim := keyClaim; claim != "" {
			keyFn = func(r *http.Request) string {
				if c := rbac.FromContext(r.Context()); c != nil {
					if v, ok := c[claim]; ok {
						if s, ok := v.(string); ok && s != "" {
							return claim + ":" + s
						}
					}
				}
				return infrahttp.ClientIP(r)
			}
		}
		mw := tb.MiddlewareWithFail(keyFn, rateOnFail)
		wrappedHandler = mw(wrappedHandler).ServeHTTP
	}

	// Per-route max-body limit. The unified `limits[case=body_too_large]`
	// wins over the legacy max_request_size + on_request_too_large pair.
	if bcfg, err := s.resolveBodyLimit(route); err != nil {
		return fmt.Errorf("route=%q body limit: %w", pattern, err)
	} else if bcfg != nil {
		log.Printf("route=%q body limit max=%dB onFail=%v", pattern, bcfg.MaxBytes, bcfg.OnFail != nil)
		mw := infrahttp.BodyLimitMiddleware(*bcfg)
		wrappedHandler = mw(wrappedHandler).ServeHTTP
	}

	// limits[case=error] wraps OUTERMOST among status-aware middlewares
	// so cache HITs (200) pass through and cache MISSes that errored
	// get swapped before they reach the wire. Wraps INSIDE CORS so
	// OPTIONS preflights still respond correctly.
	if entry, onFail := s.limitFor(route, CaseError); entry != nil && onFail != nil {
		log.Printf("route=%q limits[error] codes=%v range=%d-%d",
			pattern, entry.StatusCodes, entry.StatusMin, entry.StatusMax)
		mw := errorCaseMiddleware(entry, onFail)
		wrappedHandler = mw(wrappedHandler).ServeHTTP
	}

	if len(route.CorsOrigins) > 0 {
		corsOrigins := route.CorsOrigins
		prev := wrappedHandler
		wrappedHandler = func(w http.ResponseWriter, r *http.Request) {
			// Preflight: short-circuit the handler entirely.
			// We answer OPTIONS unconditionally when cors_origins is
			// configured — even without an Origin header (curl,
			// same-origin probes) — because the inner allowedMethods
			// check would otherwise 405 the preflight and break
			// browser CORS for the route's real verbs.
			if r.Method == http.MethodOptions {
				if connections.HandleCORS(w, r, corsOrigins) {
					return
				}
				if m := r.Header.Get("Access-Control-Request-Method"); m != "" {
					w.Header().Set("Access-Control-Allow-Methods", m+", OPTIONS")
				} else {
					w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
				}
				if h := r.Header.Get("Access-Control-Request-Headers"); h != "" {
					w.Header().Set("Access-Control-Allow-Headers", h)
				} else {
					w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
				}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			// For real requests we wrap the writer so the configured
			// CORS headers WIN over anything the upstream / inner
			// handler emits. Without this, proxy routes that pass
			// through an upstream that ALSO sets CORS produce duplicate
			// Access-Control-Allow-Origin headers (cors-proxy example).
			cw := newCORSResponseWriter(w, r, corsOrigins)
			prev(cw, r)
			cw.commitHeaders()
		}
	}

	log.Printf("finalizing route registration: %q", pattern)
	s.mux.HandleFunc(pattern, wrappedHandler)

	return nil
}

func NewStaticServer(path string, args []string) (*Server, error) {
	var server Server
	argCmd := flag.NewFlagSet("args", flag.ExitOnError)
	host := argCmd.String("host", "localhost", "Host to listen on")
	port := argCmd.Int("port", 12344, "Port to listen on")
	relPath := argCmd.String("path", "/", "url path. defaults to '/'")

	fmt.Println(path)
	fmt.Println(args)

	if err := argCmd.Parse(args[1:]); err != nil {
		return nil, fmt.Errorf("failed to parse args: %v", err.Error())
	}

	server.Config = &Config{
		Routes: []*Route{
			{
				Type: "static",
				Path: *relPath,
				StaticDirConfig: &routes.StaticConfig{
					Dir: path,
				},
			},
		},
	}

	fmt.Println("len(config): ", len(server.Config.Routes))

	hostIp := net.ProcessHost(*host)

	address := fmt.Sprintf("%s:%v", hostIp, *port)

	fmt.Println("ADDRESS: ", address)

	server.Address = address
	server.Args = map[string]string{}
	mux := http.NewServeMux()
	server.mux = mux

	return &server, nil
}

func NewHTMLServer(path string, args []string) (*Server, error) {
	var server Server
	argCmd := flag.NewFlagSet("args", flag.ExitOnError)
	static := argCmd.String("static", "", "Static [dir]:[path]")
	host := argCmd.String("host", "localhost", "Host to listen on")
	port := argCmd.Int("port", 2344, "Port to listen on")

	// err := os.Chdir(filepath.Dir(path))
	// if err != nil {
	// 	return nil, err
	// }

	fmt.Println(path)
	fmt.Println(args)

	if err := argCmd.Parse(args[1:]); err != nil {
		return nil, fmt.Errorf("failed to parse args: %v", err.Error())
	}

	config := Config{
		Routes: []*Route{},
	}

	config.Routes = append(config.Routes, &Route{
		Type: "file",
		Path: "/",
		FileConfig: &routes.FileConfig{
			FilePath: path,
		},
	})
	parts := strings.SplitN(*static, ":", 2)
	switch len(parts) {
	case 1:
		config.Routes = append(config.Routes, &Route{
			Type: "static",
			Path: "/static",
			StaticDirConfig: &routes.StaticConfig{
				Dir: strings.TrimSpace(parts[0]),
			},
		})
	case 2:
		config.Routes = append(config.Routes, &Route{
			Type: "static",
			Path: strings.TrimSpace(parts[1]),
			StaticDirConfig: &routes.StaticConfig{
				Dir: strings.TrimSpace(parts[0]),
			},
		})
	}

	server.Config = &config

	hostIp := net.ProcessHost(*host)

	address := fmt.Sprintf("%s:%v", hostIp, *port)

	fmt.Println("ADDRESS: ", address)

	server.Address = address
	server.Args = map[string]string{}
	mux := http.NewServeMux()
	server.mux = mux

	return &server, nil
}

type HandleFuncWrapper struct {
	HandlerFunc func(http.ResponseWriter, *http.Request)
}

func NewHandleFuncWrapper(handlerFunc func(http.ResponseWriter, *http.Request)) *HandleFuncWrapper {
	return &HandleFuncWrapper{
		HandlerFunc: handlerFunc,
	}
}

func (h *HandleFuncWrapper) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.HandlerFunc(w, r)
}

func NewServer(configPath string) (*Server, error) {

	os.Chdir(filepath.Dir(configPath))
	config, err := loadConfig(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load config: %w", err)
	}

	mux := http.NewServeMux()

	return &Server{Config: config, mux: mux}, nil
}

func (s *Server) InitDependencies() error {
	config := s.Config

	if config.Storage != nil {
		err := storage.InitStorage(config.Storage)
		if err != nil {
			fmt.Println("InitStorage err: ", err.Error())
			return err
		}
		// Wire storage lookup functions for task routes and the scheduler.
		// These must defer to storageaccess.GetStorageFn at call time, not
		// snapshot it now: WireAll() (which actually assigns
		// storageaccess.GetStorageFn) runs later in InitDependencies, so a
		// direct copy here would capture a nil func. Mirrors the
		// GetConnectionFn / GetPluginFn closure pattern below.
		taskroute.GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
			return storageaccess.GetStorageFn(name)
		}
		schedule.GetStorageFn = func(name string) (storageaccess.StorageRef, bool) {
			return storageaccess.GetStorageFn(name)
		}
	}

	if config.Auth != nil {
		err := auth.InitAuthManager(config.Auth)
		if err != nil {
			return err
		}
	}

	if len(config.Plugins) > 0 {
		preg, err := plugins.NewRegistry(config.Plugins)
		if err != nil {
			return fmt.Errorf("plugin registry: %w", err)
		}
		plugins.SetDefault(preg)
		log.Printf("plugin registry initialized: %d plugin(s)", len(config.Plugins))

		// Periodic health probes for HTTP plugins; results show up in the
		// admin dashboard.
		hm := plugins.NewHealthMonitor(config.Plugins)
		plugins.SetDefaultHealthMonitor(hm)
		hm.Start(context.Background(), 30*time.Second)
	}

	if len(config.Connections) > 0 {
		creg, err := connections.NewRegistry(config.Connections)
		if err != nil {
			return fmt.Errorf("connection registry: %w", err)
		}
		connections.SetDefault(creg)
		log.Printf("connection registry initialized: %d connection(s)", len(config.Connections))
	}

	// Wire httpclient registry.
	if config.Requests != nil {
		reg, err := httpclient.NewRegistry(config.Requests)
		if err != nil {
			return fmt.Errorf("requests registry: %w", err)
		}
		httpclient.SetDefault(reg)
	} else {
		httpclient.SetDefault(&httpclient.Registry{})
	}

	// Wire schedule package connection and plugin dependencies.
	schedule.GetConnectionFn = func(name string) (*connections.Broker, bool) {
		reg := connections.Default()
		if reg == nil {
			return nil, false
		}
		return reg.Get(name)
	}
	schedule.GetPluginFn = func(name string) (plugins.Client, bool) {
		reg := plugins.Default()
		if reg == nil {
			return nil, false
		}
		return reg.Get(name)
	}

	// If durable outbox is requested, open it and bind it to
	// stream-publish.forward_url so deliveries survive restarts.
	if config.OutboxDB != "" {
		if err := s.startOutbox(); err != nil {
			return fmt.Errorf("outbox: %w", err)
		}
	}

	// Spin up the in-process scheduler if any jobs are configured. Must
	// run after the plugin registry is populated.
	if err := s.startScheduler(); err != nil {
		return fmt.Errorf("scheduler: %w", err)
	}

	// Wire mailer / sms senders, verify token Issuer, and TOTP store
	// hooks so the magic-link / email-verify / 2FA route types work.
	if err := s.initAuthFlows(); err != nil {
		return fmt.Errorf("auth_flows: %w", err)
	}

	// Bind usecases injected-function variables to their concrete feature impls.
	orchusecases.WireAll()

	return nil
}

// ProbeConfig parses and fully resolves a config (externs, includes,
// prefixes, kind-reject) WITHOUT the os.Chdir side effect NewServer
// performs and WITHOUT booting anything (no InitDependencies, no DB
// open, no listeners). The resolver is CWD-independent by design, so a
// long-running multi-project process (Studio) can call this safely to
// get the exact composed view the running server would expose.
// Returns the resolver's "kind:X library, not a server" error verbatim
// for typed-library files so callers can surface it.
func ProbeConfig(configPath string) (*Config, error) {
	return loadConfig(configPath)
}

func loadConfig(configPath string) (*Config, error) {

	bytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	configYAML := string(bytes)

	// Resolve ${ENV:X}, ${FILE:/path}, and any registered custom secret
	// markers before YAML parsing so they can appear inside any scalar
	// (URLs, paths, secrets, plugin commands, etc.).
	configYAML, err = secrets.Expand(configYAML)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve secrets: %w", err)
	}

	var config Config
	err = yaml.Unmarshal([]byte(configYAML), &config)
	if err != nil {
		fmt.Println(configYAML)
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	absConfigPath, err := filepath.Abs(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve config path: %w", err)
	}
	if real, err := filepath.EvalSymlinks(absConfigPath); err == nil {
		absConfigPath = real
	}
	baseDir := filepath.Dir(absConfigPath)

	if config.Kind != "" {
		return nil, fmt.Errorf("%s is a kind:%s library, not a server — libraries are borrowed via extern:, not booted", absConfigPath, config.Kind)
	}

	if err := resolveConfig(&config, absConfigPath, baseDir); err != nil {
		return nil, err
	}

	return &config, nil
}

type RouteSummary struct {
	Path        string `json:"path"`
	Type        string `json:"type"`
	Description string `json:"description"`
	Method      string `json:"method"`
}

// renderVars materializes routes from the resolver-prepared route
// groups, applying $arg/$env substitution and composition prefixes.
// All the heavy lifting (and the only place prefixing happens) lives
// in materializeRoutes so `wave routes` / `wave validate` / openapi
// converge on the identical merged+prefixed route set.
func (s *Server) renderVars() error {
	return materializeRoutes(s.Config, s.Args)
}

func (s *Server) Start(ctx context.Context) error {
	if s.Config.Build != nil {
		if err := bundler.Run(*s.Config.Build); err != nil {
			return fmt.Errorf("frontend build failed: %w", err)
		}
		if s.Config.Build.Watch {
			bundler.StartWatcher(*s.Config.Build, ctx)
		}
		s.Config.Routes = append(s.Config.Routes, &Route{
			Type: "static",
			Path: "/",
			StaticDirConfig: &routes.StaticConfig{Dir: s.Config.Build.DistDir},
		})
	}

	err := s.renderVars()
	if err != nil {
		return err
	}

	err = s.InitDependencies()
	if err != nil {
		return err
	}

	// Phase 4: second-pass secret resolution. Plugins are now built, so
	// ${PLUGIN:name:uri} markers preserved through the first
	// loadConfig pass can be resolved against the secrets-kind plugin
	// registry. Must run before downstream features start consuming
	// the config (storage validate, auth validate, route handlers).
	if err := s.installSecretsPluginResolver(); err != nil {
		return err
	}

	// Phase 5: wire the unified Sink (Prometheus + plugin exporter
	// fan-out). After plugins are built so kinds.LoadExporter sees
	// them; before validators so any startup events / errors land
	// somewhere observable.
	if err := s.bootstrapObservability(); err != nil {
		return err
	}

	if err := s.validateStorageRefs(); err != nil {
		return err
	}

	if err := s.validateAuthRefs(); err != nil {
		return err
	}

	if err := s.resolveAndRegisterAuthPlugins(); err != nil {
		return err
	}

	address := s.Address
	args := s.Args

	// common.PrintJSON(common.Object{"INNER": s.Config})

	fmt.Println("ROUTES count: ", len(s.Config.Routes))

	routes := []RouteSummary{}

	for _, route := range s.Config.Routes {

		err := route.render(args)
		if err != nil {
			return err
		}

		method := strings.ToUpper(strings.TrimSpace(route.Method))
		if method == "" {
			method = "GET"
		}

		routes = append(routes, RouteSummary{
			Path:        route.Path,
			Type:        route.Type,
			Description: route.Description,
			Method:      method,
		})

		fmt.Printf("Setting up path: '%s' for '%s'\n", route.Path, route.Type)

		err = route.setRouteConfig()
		if err != nil {
			return err
		}

		// Resolve every name in route.Limits against the top-level
		// Config.Limits registry into route.resolvedLimits (case→entry).
		// This must run before HandleFunc since the middleware chain
		// reads from the resolved map.
		if err := route.resolveLimits(s.Config.Limits); err != nil {
			return fmt.Errorf("route %q: %w", route.Path, err)
		}

		err = route.Validate()
		if err != nil {
			return err
		}

		err = s.HandleFunc(route)
		if err != nil {
			return err
		}
	}

	// Auto-register GET <subscribe_path> for every connection.
	s.registerSubscribeRoutes()

	// Health endpoints — always registered, no config required.
	registerHealthRoutes(s.mux)

	// Stream-publish discovery: emit any route_id → endpoint metadata so
	// frontends can discover SSE entrypoints without hardcoding paths.
	s.registerStreamDiscovery()

	// Prometheus-format metrics. Lazy-attaches broker gauges so this
	// must run after registerSubscribeRoutes.
	s.registerMetricsEndpoint()

	// Admin dashboard at /admin — read-only HTML view of routes,
	// brokers, plugins, and metrics. Auto-refreshes every 5s.
	s.registerAdminDashboard()

	// OpenAPI 3 spec generated from the loaded route table.
	s.registerOpenAPI()

	// Hand-rolled HTML viewer for the OpenAPI spec at /docs.
	s.registerDocsViewer()

	// Catch-all for unmatched paths. Registered LAST so the bare "/"
	// pattern can't shadow any more-specific route registered above.
	if err := s.registerNotFound(args); err != nil {
		return err
	}

	if s.Config.JSONDiscoveryRoutePath != "" {
		w := strings.Builder{}
		encoder := json.NewEncoder(&w)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "    ")
		data, _ := json.Marshal(routes)
		s.mux.HandleFunc(s.Config.JSONDiscoveryRoutePath, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "application/json")
			w.Write(data)
		})
	}
	
	if s.Config.HTMLDiscoveryRoutePath != "" {

		s.mux.HandleFunc(s.Config.HTMLDiscoveryRoutePath, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("content-type", "text/html")
			renderDiscovery(w, routes)
		})
	}

	// if s.debug {
	// 	s.initDebug()
	// }

	var handler http.Handler

	if s.Config.IpFilter != nil {
		filter := ipfilter.NewIPFilterCombined(s.Config.IpFilter.Whitelist, s.Config.IpFilter.Blacklist)

		handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			allowed, clientIP := filter.CheckRequest(r)
			if !allowed {
				log.Printf("Blocked request from IP: %s", clientIP)
				http.Error(w, "Access denied", http.StatusForbidden)
				return
			}
			s.mux.ServeHTTP(w, r)
		})
	} else {
		handler = s.mux
	}

	// Outer middleware chain — order matters: request-id outermost so it
	// shows up in every log line, then security headers, then body limit,
	// then access logging, then a per-request counter bump.
	countingMW := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestsTotal.Add(1)
			start := time.Now()
			rw := &statusCapturingWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			// Emit through the unified observability sink (Prometheus +
			// fan-out to plugin exporters). Non-blocking by contract.
			labels := map[string]string{
				"method": r.Method,
				"status": fmt.Sprintf("%d", rw.status),
			}
			observability.Default().EmitMetric(&observability.Sample{
				Name:   "wave_http_requests_total",
				Type:   "counter",
				Value:  1,
				Labels: labels,
			})
			observability.Default().EmitMetric(&observability.Sample{
				Name:   "wave_http_request_duration_seconds",
				Type:   "histogram",
				Value:  time.Since(start).Seconds(),
				Labels: labels,
			})
		})
	}
	handler = infrahttp.Chain(
		errreport.RecoveryMiddleware,                 // outermost: catch panics
		infrahttp.RequestIDMiddleware,
		infrahttp.SecurityHeadersMiddleware(infrahttp.SecurityHeadersConfig{
			HSTS: "max-age=31536000; includeSubDomains",
		}),
		infrahttp.MaxBodyMiddleware(0), // 16 MiB default
		infrahttp.GzipMiddleware,       // negotiated; opts out of streaming/SSE
		countingMW,
		loggingMiddleware,
	)(handler)

	srv := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       120 * time.Second,
	}

	// Graceful shutdown: cancel on SIGINT/SIGTERM in addition to ctx.
	if ctx == nil {
		ctx = context.Background()
	}
	shutdownCtx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-shutdownCtx.Done()
		log.Printf("shutdown signal received; draining (max 10s)")
		drainCtx, drainCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer drainCancel()
		_ = srv.Shutdown(drainCtx)
		_ = s.Stop()
		cancel()
	}()

	markReady()
	if s.Config.HTTPSConfig != nil {
		if s.Config.HTTPSConfig.Generate && !common.PathExists(s.Config.HTTPSConfig.SSLCertfile) {
			err = generateHTTPS(s.Config.HTTPSConfig)
			if err != nil {
				return err
			}
		}
		log.Printf("Server starting https://%s", address)
		err = srv.ListenAndServeTLS(
			s.Config.HTTPSConfig.SSLCertfile,
			s.Config.HTTPSConfig.SSLKeyfile,
		)
	} else {
		log.Printf("Server starting http://%s", address)
		err = srv.ListenAndServe()
	}

	// Ignore "server closed" errors
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// Stop releases process-wide resources owned by the server. Currently
// drains the observability fanout so in-flight batches reach their
// plugin exporters before the process exits. Idempotent.
func (s *Server) Stop() error {
	if s.fanout != nil {
		return s.fanout.Close()
	}
	return nil
}

// var SSR_SCRIPT_PATH string

// func (s *Server) initDebug() {
// 	SSR_SCRIPT_PATH = InitSSrFileChange(s)
// }
