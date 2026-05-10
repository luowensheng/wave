package servers

import (
	"strings"

	"gopkg.in/yaml.v3"
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
	cfg := s.Config

	// loadConfig keeps routes as RawRoutes; materialize them now.
	if len(cfg.Routes) == 0 {
		if b, err := cfg.RawRoutes.Bytes(); err == nil && len(b) > 0 {
			if err := yaml.Unmarshal(b, &cfg.Routes); err != nil {
				return nil, err
			}
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
