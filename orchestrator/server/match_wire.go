package servers

import (
	"fmt"
	"net/http"

	"gopkg.in/yaml.v3"
)

// buildMatchSubHandler resolves a `type: match` case's `route:`
// field into a fully wrapped http.HandlerFunc. The field is either:
//
//   - string: an id reference. Looked up in s.routesById. The
//     referenced route has already been render()/setRouteConfig()/
//     Validate()'d at boot, so the only work left is CreateRoute +
//     wrapRouteMiddleware.
//
//   - map (decoded YAML): a fresh inline route. YAML round-trip into
//     a new *Route, run through the same boot pipeline (render →
//     setRouteConfig → resolveLimits → Validate), then wrap.
//
// Used as the implementation of match.BuildSubHandlerFn (wired in
// Server.Start). Errors here bubble up to abort server start, so
// invalid match-route configs never reach traffic.
func (s *Server) buildMatchSubHandler(routeOrId any) (http.HandlerFunc, error) {
	switch v := routeOrId.(type) {
	case string:
		r, ok := s.routesById[v]
		if !ok {
			return nil, fmt.Errorf("unresolved route id %q", v)
		}
		return s.wrapResolvedRoute(r)
	case *Route:
		return s.wrapResolvedRoute(v)
	}

	// Inline map: round-trip through YAML into a fresh *Route.
	raw, err := yaml.Marshal(routeOrId)
	if err != nil {
		return nil, fmt.Errorf("inline route: marshal: %w", err)
	}
	r := &Route{}
	if err := yaml.Unmarshal(raw, r); err != nil {
		return nil, fmt.Errorf("inline route: unmarshal: %w", err)
	}
	if err := r.render(s.Args); err != nil {
		return nil, fmt.Errorf("inline route: render: %w", err)
	}
	if err := r.setRouteConfig(); err != nil {
		return nil, fmt.Errorf("inline route: %w", err)
	}
	if err := r.resolveLimits(s.Config.Limits); err != nil {
		return nil, fmt.Errorf("inline route: %w", err)
	}
	// Note: deliberately skip r.Validate() here — inline match-case
	// routes have neither path nor id (they exist only as case
	// targets) so the path-or-id check doesn't apply.
	return s.wrapResolvedRoute(r)
}

// wrapResolvedRoute calls CreateRoute on a fully-boot-prepared route
// and wraps the result in the standard per-route middleware chain.
func (s *Server) wrapResolvedRoute(r *Route) (http.HandlerFunc, error) {
	if r.config == nil {
		// Defensive: should have been set by setRouteConfig at boot.
		if err := r.setRouteConfig(); err != nil {
			return nil, err
		}
	}
	handler, err := r.config.CreateRoute(r.Method, r.Path, s.Args)
	if err != nil {
		return nil, fmt.Errorf("CreateRoute: %w", err)
	}
	return s.wrapRouteMiddleware(r, handler)
}
