package servers

import (
	"net/http"

	"github.com/luowensheng/wave/orchestrator/features/auth"
)

// currentUserIDFromContext returns the username of the authenticated
// caller, or empty when the request is unauthenticated.
//
// We pull from auth.UserContextKey which is what features/auth's
// RequireAuth populates — so every TOTP route automatically respects
// whatever auth: [...] config the user attached to it.
func currentUserIDFromContext(r *http.Request) string {
	if u, ok := r.Context().Value(auth.UserContextKey).(*auth.PublicUser); ok && u != nil {
		return u.Username
	}
	return ""
}
