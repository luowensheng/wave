package servers

import (
	infrahttp "wave/infra/http"
	"net/http"
)

// csrfStore is the process-level CSRF token store (one per server process).
// In VHCO this would be passed explicitly; kept here for backward compatibility
// while the full orchestrator DI wiring is being introduced.
var csrfStore = infrahttp.NewCSRFStore()

func init() {
	csrfStore.StartCleanup()
}

func (s *Server) csrfMiddleware(next http.Handler) http.HandlerFunc {
	return http.HandlerFunc(csrfStore.Middleware(next).ServeHTTP)
}

func (s *Server) ValidateCSRFToken(w http.ResponseWriter, r *http.Request) error {
	return csrfStore.Validate(w, r)
}

func (s *Server) IncludeCSRFToken(w http.ResponseWriter, r *http.Request) string {
	return csrfStore.GenerateToken(w, r)
}

// CleanupExpiredTokens and StartTokenCleanup kept for callers that reference them.
func (s *Server) CleanupExpiredTokens() { csrfStore.CleanupExpired() }
func (s *Server) StartTokenCleanup()    { /* started in init() */ }
