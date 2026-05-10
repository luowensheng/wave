// Package rbac is a tiny claims-based authorization layer.
//
// Two atomic checks plug into HTTP middleware:
//
//	require_roles:  ["admin", "billing"]
//	require_claims: { plan: "enterprise", region: "us-east" }
//
// All required roles AND all required claims must hold for the request
// to pass. Roles are looked up in standard OIDC token shapes — a
// `roles` array, a `groups` array, or scope-style space-separated
// strings — using a small reflective walker so we don't tie ourselves
// to one IdP convention. Claims comparison is exact-string.
//
// The package is **transport-agnostic**: it operates on a generic
// `claims map[string]any`. Any auth layer (OIDC, JWT, custom) that
// stashes a claims map on the request context can use it.
package rbac

import (
	"context"
	"fmt"
	"net/http"
	"strings"
)

type ctxKey struct{}

// WithClaims tags a request context with the user's claims so a
// subsequent Middleware call can read them. Auth integrations should
// call this just before invoking the next handler.
func WithClaims(ctx context.Context, claims map[string]any) context.Context {
	return context.WithValue(ctx, ctxKey{}, claims)
}

// FromContext returns the claims stashed by WithClaims, or nil.
func FromContext(ctx context.Context) map[string]any {
	if c, ok := ctx.Value(ctxKey{}).(map[string]any); ok {
		return c
	}
	return nil
}

// Policy encodes the requirements for a single route.
type Policy struct {
	Roles  []string          // user must have all listed roles
	Claims map[string]string // exact-match key=value
}

// Empty returns true when the policy imposes no checks.
func (p Policy) Empty() bool { return len(p.Roles) == 0 && len(p.Claims) == 0 }

// Check returns nil if claims satisfy p, otherwise a descriptive error
// (suitable for logging/audit; do not return verbatim to clients).
func (p Policy) Check(claims map[string]any) error {
	if p.Empty() {
		return nil
	}
	if claims == nil {
		return fmt.Errorf("no claims on request")
	}
	missing := []string{}
	for _, role := range p.Roles {
		if !hasRole(claims, role) {
			missing = append(missing, "role:"+role)
		}
	}
	for k, want := range p.Claims {
		got, ok := claims[k]
		if !ok || asString(got) != want {
			missing = append(missing, fmt.Sprintf("claim:%s=%s", k, want))
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("missing %s", strings.Join(missing, ", "))
	}
	return nil
}

// Middleware enforces p on the wrapped handler. On rejection writes 403
// (forbidden — the caller is authenticated but lacks permissions).
func Middleware(p Policy) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		if p.Empty() {
			return next
		}
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := p.Check(FromContext(r.Context())); err != nil {
				http.Error(w, "forbidden", http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// hasRole walks the common claim shapes. Roles can live under "roles",
// "groups", "permissions", or as a space-separated "scope" string.
func hasRole(claims map[string]any, want string) bool {
	for _, key := range []string{"roles", "groups", "permissions"} {
		if list, ok := claims[key]; ok && containsString(list, want) {
			return true
		}
	}
	if scope, ok := claims["scope"].(string); ok {
		for _, p := range strings.Fields(scope) {
			if p == want {
				return true
			}
		}
	}
	return false
}

func containsString(v any, want string) bool {
	switch s := v.(type) {
	case []any:
		for _, x := range s {
			if asString(x) == want {
				return true
			}
		}
	case []string:
		for _, x := range s {
			if x == want {
				return true
			}
		}
	case string:
		return s == want
	}
	return false
}

func asString(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case fmt.Stringer:
		return s.String()
	default:
		return fmt.Sprintf("%v", v)
	}
}
