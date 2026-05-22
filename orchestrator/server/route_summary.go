package servers

import (
	"strings"
)

// RouteSummaryRow is the public, machine-readable shape returned by
// `wave routes`. Stable enough to depend on from CI scripts.
type RouteSummaryRow struct {
	Method      string   `json:"method"`
	Path        string   `json:"path"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
	Auth        []string `json:"auth,omitempty"`
}

// RouteSummaries materializes the configured routes (without booting
// the server) and returns one row per route. Variable rendering ($args
// / $env) is skipped — the goal here is structural inspection, not
// runtime substitution.
func (s *Server) RouteSummaries() ([]RouteSummaryRow, error) {
	if s == nil || s.Config == nil {
		return nil, nil
	}
	return RouteSummariesFromConfig(s.Config)
}

// RouteSummariesFromConfig is the Config-level route inspector. It
// materializes the merged + prefixed route set (composition-aware:
// includes folded in, prefixes applied, externs already resolved by
// the resolver in loadConfig) and returns one row per route. Used by
// `wave routes`, the Studio UI, and any surface that must reflect the
// same composed view the running server exposes — single source of
// truth, no per-consumer YAML re-walking.
func RouteSummariesFromConfig(cfg *Config) ([]RouteSummaryRow, error) {
	if cfg == nil {
		return nil, nil
	}
	// Structural inspection: args==nil leaves $x literal (no $arg/$env).
	if !cfg.routesMaterialized {
		if err := materializeRoutes(cfg, nil); err != nil {
			return nil, err
		}
	}

	rows := make([]RouteSummaryRow, 0, len(cfg.Routes))
	for _, r := range cfg.Routes {
		method := strings.ToUpper(strings.TrimSpace(r.Method))
		rows = append(rows, RouteSummaryRow{
			Method:      method,
			Path:        r.Path,
			Type:        r.Type,
			Description: r.Description,
			Auth:        r.Auth,
		})
	}
	return rows, nil
}
