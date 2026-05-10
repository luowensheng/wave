package servers

import (
	"encoding/json"
	"net/http"
	"strings"
)

// OpenAPI 3.1 export — generated from the loaded route table at boot.
//
// This is intentionally a *minimal* spec: we don't know request/response
// schemas for arbitrary routes, but we DO know enough to give clients,
// load tests, and import-into-Postman/Insomnia a useful starting point:
// path, method, summary (from `description`), tags (from `type`), and
// security requirements (from `auth`). Route-type-specific metadata
// (e.g. stream-publish event_type, plugin trigger_key) lives in
// vendor extensions so consumers can skip what they don't understand.

// registerOpenAPI installs GET /openapi.json. The spec is built once at
// boot — fast scrapes don't re-walk the route table.
func (s *Server) registerOpenAPI() {
	body, err := s.buildOpenAPI()
	if err != nil {
		return // best-effort; never block boot
	}
	s.mux.HandleFunc("GET /openapi.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
}

func (s *Server) buildOpenAPI() ([]byte, error) {
	paths := map[string]map[string]any{}

	for _, route := range s.Config.Routes {
		// Skip route patterns that http.ServeMux dispatches but that
		// don't make sense to advertise (admin/health/metrics are
		// registered separately and intentionally undocumented here —
		// they're operational endpoints).
		if route.Path == "" {
			continue
		}

		method := strings.ToLower(strings.TrimSpace(route.Method))
		if method == "" {
			method = "get"
		}

		op := map[string]any{
			"summary":     firstNonEmpty(route.Description, route.Type),
			"description": route.Description,
			"tags":        []string{route.Type},
			"x-wave-type": route.Type,
		}
		if len(route.Auth) > 0 {
			sec := []map[string][]string{}
			for _, name := range route.Auth {
				sec = append(sec, map[string][]string{name: {}})
			}
			op["security"] = sec
		}

		// Type-specific extensions.
		switch route.Type {
		case "stream-publish":
			if sp := route.StreamPublishConfig; sp != nil {
				op["x-wave-stream"] = map[string]any{
					"connection": sp.Connection,
					"route_id":   sp.RouteID,
					"event_type": sp.EventType,
				}
			}
		case "plugin":
			if p := route.PluginConfig; p != nil {
				op["x-wave-plugin"] = map[string]any{
					"name":        p.Name,
					"trigger_key": p.TriggerKey,
				}
			}
		case "forward", "dynamic-forward":
			if f := route.ForwardConfig; f != nil {
				op["x-wave-forward-url"] = f.ForwardURL
			}
		}

		if _, ok := paths[route.Path]; !ok {
			paths[route.Path] = map[string]any{}
		}
		paths[route.Path][method] = op

		// Many routes accept multiple methods via `methods`.
		for _, m := range route.Methods {
			m = strings.ToLower(strings.TrimSpace(m))
			if m == "" || m == method {
				continue
			}
			paths[route.Path][m] = op
		}
	}

	// Auto-document the operational endpoints we register elsewhere so
	// consumers see the full surface.
	add := func(path, method, summary, tag string) {
		if _, ok := paths[path]; !ok {
			paths[path] = map[string]any{}
		}
		paths[path][method] = map[string]any{
			"summary": summary,
			"tags":    []string{tag},
		}
	}
	add("/healthz", "get", "Liveness probe", "ops")
	add("/readyz", "get", "Readiness probe", "ops")
	add("/version", "get", "Build version", "ops")
	add("/metrics", "get", "Prometheus text metrics", "ops")
	add("/admin/", "get", "Admin dashboard", "ops")

	host := ""
	if s.Config.Defaults != nil && s.Config.Defaults.Host != nil {
		host = *s.Config.Defaults.Host
	}

	// Build security schemes from configured auth types.
	schemes := map[string]map[string]any{}
	for name, ac := range s.Config.Auth {
		sch := map[string]any{}
		switch strings.ToLower(ac.TokenLocation) {
		case "header":
			sch["type"] = "http"
			sch["scheme"] = strings.ToLower(firstNonEmpty(ac.HeaderScheme, "bearer"))
		case "cookie":
			sch["type"] = "apiKey"
			sch["in"] = "cookie"
			sch["name"] = ac.CookieName
		default:
			sch["type"] = "http"
			sch["scheme"] = "bearer"
		}
		schemes[name] = sch
	}

	doc := map[string]any{
		"openapi": "3.1.0",
		"info": map[string]any{
			"title":       "wave",
			"version":     Version,
			"description": "Auto-generated from server.yaml routes. Type-specific metadata in `x-wave-*` extensions.",
		},
		"paths": paths,
	}
	if host != "" {
		doc["servers"] = []map[string]any{{"url": "http://" + host}}
	}
	if len(schemes) > 0 {
		doc["components"] = map[string]any{"securitySchemes": schemes}
	}
	return json.MarshalIndent(doc, "", "  ")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
