// Package match implements the `type: match` route — a declarative
// per-request router that dispatches a single path to one of several
// nested routes based on predicates evaluated against the incoming
// request (method, headers, cookies, query, host, client IP, path
// vars).
//
// Cases are evaluated in declaration order, first match wins. If
// none match and a `default` is set, it runs; otherwise the matcher
// returns 404.
//
// Each case's nested route carries its own middleware (auth, inputs,
// CORS, csrf, etc.). The wrapping is provided by the orchestrator
// through WrapMiddlewareFn / BuildSubHandlerFn injection — the match
// package does not depend on orchestrator/server, breaking the cycle.
package match

import (
	"errors"
	"fmt"
	"net/http"
)

// Config is the YAML shape under `match:`.
type Config struct {
	Cases   []Case `yaml:"cases,omitempty" json:"cases,omitempty"`
	Default *Case  `yaml:"default,omitempty" json:"default,omitempty"`
}

// Case is one branch of a match route.
type Case struct {
	// When names the dimension to inspect. Exactly one of:
	// "method" | "host" | "ip" | "header" | "cookie" | "query" | "path".
	// Empty on the `default` case.
	When string `yaml:"when,omitempty" json:"when,omitempty"`

	// Match is the criteria for that dimension. Shape depends on When:
	//   keyed dimensions   (header/cookie/query/path) → map[string]any
	//   scalar dimensions  (method/host/ip)           → string OR MatchOp
	// Values inside keyed-dimension maps are themselves string OR MatchOp.
	// Plain string values are shorthand for { equals: "..." }.
	Match any `yaml:"match,omitempty" json:"match,omitempty"`

	// Route is the nested route definition — either:
	//   - string: an id reference to another route declared elsewhere.
	//   - map:    a full inline route definition.
	// Resolved at boot time by BuildSubHandlerFn.
	Route any `yaml:"route,omitempty" json:"route,omitempty"`
}

// MatchOp is the structured operator form. Plain string values in
// YAML are shorthand for { equals: "..." }.
type MatchOp struct {
	Equals string `yaml:"equals,omitempty" json:"equals,omitempty"`
	Regex  string `yaml:"regex,omitempty"  json:"regex,omitempty"`
	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`
	Exists *bool  `yaml:"exists,omitempty" json:"exists,omitempty"`
}

// CreateRoute implements the route-config contract. At boot it
// compiles every case (including regex compile) and resolves each
// case's `route:` (by id or inline) into a fully wrapped sub-handler.
// At request time the returned handler walks compiled cases in order;
// first match wins; falls through to default; 404 otherwise.
func (c *Config) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {
	if BuildSubHandlerFn == nil {
		return nil, errors.New("match: BuildSubHandlerFn not wired — orchestrator did not call InitDependencies")
	}
	if len(c.Cases) == 0 && c.Default == nil {
		return nil, errors.New("match: at least one case or a default is required")
	}

	cases := make([]compiledCase, 0, len(c.Cases))
	for i, cc := range c.Cases {
		compiled, err := compileCase(cc, fmt.Sprintf("cases[%d]", i))
		if err != nil {
			return nil, err
		}
		cases = append(cases, compiled)
	}

	var defaultHandler http.HandlerFunc
	if c.Default != nil {
		h, err := BuildSubHandlerFn(c.Default.Route)
		if err != nil {
			return nil, fmt.Errorf("match: default route: %w", err)
		}
		defaultHandler = h
	}

	return func(w http.ResponseWriter, r *http.Request) {
		for _, cc := range cases {
			if cc.eval(r) {
				cc.handler(w, r)
				return
			}
		}
		if defaultHandler != nil {
			defaultHandler(w, r)
			return
		}
		// No case matched and no `default:` declared — emit the
		// framework-standard 404 shape so every not-found in Wave
		// (server-wide catch-all, match no-match, future handlers)
		// returns the same JSON envelope.
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(http.StatusNotFound)
		fmt.Fprintf(w, `{"error":"page not found","path":%q}`+"\n", r.URL.Path)
	}, nil
}
