package match

import "net/http"

// BuildSubHandlerFn resolves a match case's `route:` field — which is
// either a string (id reference to a route declared elsewhere) or a
// map (inline route YAML) — into a fully wrapped http.HandlerFunc
// (CreateRoute + middleware chain applied per-case).
//
// Wired by the orchestrator at boot. Returns an error on unresolved
// ids, decode failures, or invalid nested route configs.
var BuildSubHandlerFn func(routeOrId any) (http.HandlerFunc, error)
