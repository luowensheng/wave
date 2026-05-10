package kinds

import "context"

// JSON-RPC method names exposed by auth-kind plugins.
const (
	MethodAuthAuthenticate   = "auth.authenticate"
	MethodAuthRefreshClaims  = "auth.refresh_claims"
	MethodAuthLogout         = "auth.logout"
)

// AuthPlugin is the typed interface for KindAuth plugins. It generalises
// oauth.Provider so non-OAuth identity sources (LDAP, SAML, custom JWT
// bridges) can plug in without retrofitting the OAuth-specific shape.
type AuthPlugin interface {
	Authenticate(ctx context.Context, req *AuthRequest) (*AuthResult, error)
	RefreshClaims(ctx context.Context, subject string) (*Claims, error)
	Logout(ctx context.Context, subject string) error
	Close() error
}

// AuthRequest describes one authentication attempt. The Method field
// disambiguates flows ("password", "token", "oauth_callback", ...).
type AuthRequest struct {
	Method      string            `json:"method"`
	Credentials map[string]string `json:"credentials,omitempty"`
	Headers     map[string]string `json:"headers,omitempty"`
	Cookies     map[string]string `json:"cookies,omitempty"`
}

// AuthResult is what a plugin returns from Authenticate. Redirect is set
// for browser-driven flows (OAuth), SetCookies lets the plugin install
// session state without orchestrator-side bookkeeping.
type AuthResult struct {
	Authenticated bool      `json:"authenticated"`
	Claims        *Claims   `json:"claims,omitempty"`
	Redirect      string    `json:"redirect,omitempty"`
	SetCookies    []*Cookie `json:"set_cookies,omitempty"`
}

// Claims is the normalized identity payload an auth plugin produces.
type Claims struct {
	Subject       string         `json:"subject"`
	Email         string         `json:"email,omitempty"`
	EmailVerified bool           `json:"email_verified,omitempty"`
	Name          string         `json:"name,omitempty"`
	Roles         []string       `json:"roles,omitempty"`
	Scopes        []string       `json:"scopes,omitempty"`
	Provider      string         `json:"provider,omitempty"`
	Raw           map[string]any `json:"raw,omitempty"`
}

// Cookie is the wire form of an HTTP cookie — kept dependency-free so
// the SDK module can mirror it without importing net/http.
type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Path     string `json:"path,omitempty"`
	Domain   string `json:"domain,omitempty"`
	MaxAge   int    `json:"max_age,omitempty"`
	Secure   bool   `json:"secure,omitempty"`
	HTTPOnly bool   `json:"http_only,omitempty"`
	SameSite string `json:"same_site,omitempty"`
}
