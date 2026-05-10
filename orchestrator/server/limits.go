package servers

import "fmt"

// LimitEntry is one named entry in the top-level `Config.Limits`
// registry. Each entry names a CASE (the kind of failure to intercept)
// and provides:
//   - Trigger config (case-specific; only the relevant fields are
//     read per case — the rest are ignored).
//   - `on_fail`: how to respond when the case fires.
//
// Routes pull entries by name via Route.Limits []string. Each route
// may resolve to at most one entry per Case (later names in the list
// override earlier ones — see resolveLimits).
//
// Cases:
//
//	body_too_large    → request body exceeds MaxSize
//	invalid_inputs    → declared inputs fail validation
//	rate_limited      → exceeded RPS
//	circuit_open      → upstream circuit breaker is open
//	unauthenticated   → missing / invalid credentials
//	forbidden         → RBAC denial
//	missing_signature → webhook signature failure
//	error             → any 4xx/5xx response from the route handler
//	                    that no other case has already intercepted.
//	                    Use StatusCodes / StatusMin / StatusMax to
//	                    narrow the trigger; defaults to 400..599.
//
// Multiple entries for the same case are not allowed.
type LimitEntry struct {
	Case   string      `yaml:"case,omitempty" json:"case,omitempty"`
	OnFail *FailAction `yaml:"on_fail,omitempty" json:"on_fail,omitempty"`

	// body_too_large
	MaxSize string `yaml:"max_size,omitempty" json:"max_size,omitempty"`

	// rate_limited
	RPS      float64 `yaml:"rps,omitempty" json:"rps,omitempty"`
	Burst    float64 `yaml:"burst,omitempty" json:"burst,omitempty"`
	KeyClaim string  `yaml:"key_claim,omitempty" json:"key_claim,omitempty"`

	// circuit_open
	FailureThreshold int    `yaml:"failure_threshold,omitempty" json:"failure_threshold,omitempty"`
	Cooldown         string `yaml:"cooldown,omitempty" json:"cooldown,omitempty"`

	// error — generic catch-all. Either an explicit list of codes
	// (StatusCodes) OR an inclusive range (StatusMin/StatusMax).
	// Empty = all of 400..599.
	StatusCodes []int `yaml:"status_codes,omitempty" json:"status_codes,omitempty"`
	StatusMin   int   `yaml:"status_min,omitempty" json:"status_min,omitempty"`
	StatusMax   int   `yaml:"status_max,omitempty" json:"status_max,omitempty"`
}

// FailAction is the response shape used by every `on_fail`. Exactly
// one of the four action fields should be set; if multiple are set,
// precedence is:
//
//	RoutePath > Redirect > TemplateFile > TemplateInline > default
//
// The default (no action set) is a JSON envelope plus the case's
// natural status code (413, 400, 429, 503, 401, 403 respectively).
type FailAction struct {
	Redirect       string            `yaml:"redirect,omitempty" json:"redirect,omitempty"`
	TemplateInline string            `yaml:"template_inline,omitempty" json:"template_inline,omitempty"`
	TemplateFile   string            `yaml:"template_file,omitempty" json:"template_file,omitempty"`
	// RoutePath delegates to another configured route by URL path.
	// At request time the failure handler dispatches the request to
	// that path through the same mux — so the target route's own
	// middleware (cache, etc.) all run.
	RoutePath string            `yaml:"route_path,omitempty" json:"route_path,omitempty"`
	Status    int               `yaml:"status,omitempty" json:"status,omitempty"`
	Headers   map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
}

// Standard case names. Exported as constants so other packages can
// reference them without typos.
const (
	CaseBodyTooLarge     = "body_too_large"
	CaseInvalidInputs    = "invalid_inputs"
	CaseRateLimited      = "rate_limited"
	CaseCircuitOpen      = "circuit_open"
	CaseUnauthenticated  = "unauthenticated"
	CaseForbidden        = "forbidden"
	CaseMissingSignature = "missing_signature"
	CaseError            = "error"
)

// findLimit returns the entry for the given case (or nil if absent).
// Reads from the resolved case→entry map populated at boot by
// resolveLimits.
func (r *Route) findLimit(name string) *LimitEntry {
	if r.resolvedLimits == nil {
		return nil
	}
	return r.resolvedLimits[name]
}

// resolveLimits walks Route.Limits (a list of names), looks each up
// in the supplied registry, and populates Route.resolvedLimits as a
// case→entry map. Last-wins on Case collisions (configuration
// cascade). Returns an error on any unknown name or on any referenced
// entry whose Case is empty / unknown.
//
// Called once per route from Server.Start before the middleware chain
// is wired.
func (r *Route) resolveLimits(registry map[string]*LimitEntry) error {
	r.resolvedLimits = map[string]*LimitEntry{}
	for _, name := range r.Limits {
		entry, ok := registry[name]
		if !ok {
			return fmt.Errorf("unknown limit %q (define it in top-level limits:)", name)
		}
		if entry.Case == "" {
			return fmt.Errorf("limit %q: missing case", name)
		}
		if !isKnownCase(entry.Case) {
			return fmt.Errorf("limit %q: unknown case %q", name, entry.Case)
		}
		// Last entry wins on collision — overwrite the map slot.
		r.resolvedLimits[entry.Case] = entry
	}
	return nil
}

// isKnownCase reports whether s matches one of the Case* constants.
// Used by resolveLimits and ValidateConfig to reject typos at boot.
func isKnownCase(s string) bool {
	switch s {
	case CaseBodyTooLarge, CaseInvalidInputs, CaseRateLimited,
		CaseCircuitOpen, CaseUnauthenticated, CaseForbidden,
		CaseMissingSignature, CaseError:
		return true
	}
	return false
}
