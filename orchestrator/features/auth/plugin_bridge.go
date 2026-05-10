// Package auth — plugin_bridge connects kind=auth plugins to the
// orchestrator's session/cookie/JWT machinery. The plugin only does
// identity (Authenticate / RefreshClaims / Logout); the orchestrator
// continues to own everything else, mirroring the OIDC and OAuth bridges.
package auth

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"wave/domain"
	"wave/infra/jwt"
)

// AuthPluginRequest mirrors infra/plugins/kinds.AuthRequest at the
// boundary so the auth package doesn't import infra/plugins/kinds
// (which would form an import cycle through io/http/contentloader).
type AuthPluginRequest struct {
	Method      string
	Credentials map[string]string
	Headers     map[string]string
	Cookies     map[string]string
}

// AuthPluginCookie mirrors infra/plugins/kinds.Cookie.
type AuthPluginCookie struct {
	Name     string
	Value    string
	Path     string
	Domain   string
	MaxAge   int
	Secure   bool
	HTTPOnly bool
	SameSite string
}

// AuthPluginClaims mirrors infra/plugins/kinds.Claims.
type AuthPluginClaims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Roles         []string
	Scopes        []string
	Provider      string
	Raw           map[string]any
}

// AuthPluginResult mirrors infra/plugins/kinds.AuthResult.
type AuthPluginResult struct {
	Authenticated bool
	Claims        *AuthPluginClaims
	Redirect      string
	SetCookies    []*AuthPluginCookie
}

// AuthPlugin is the local mirror of infra/plugins/kinds.AuthPlugin.
// Boot code in orchestrator/server adapts the kind-package interface
// into this one before calling RegisterAuthPlugins.
type AuthPlugin interface {
	Authenticate(ctx context.Context, req *AuthPluginRequest) (*AuthPluginResult, error)
	RefreshClaims(ctx context.Context, subject string) (*AuthPluginClaims, error)
	Logout(ctx context.Context, subject string) error
}

// authPlugins is the boot-time map of AuthConfig.Name → AuthPlugin. It
// mirrors the (private) caches kept by oidc_bridge.go and oauth_bridge.go.
// Populated by RegisterAuthPlugins (called from orchestrator/server boot)
// or directly by setupAuthPlugins.
var authPlugins = map[string]AuthPlugin{}

// RegisterAuthPlugins lets the boot code install the resolved plugins
// into the bridge. The orchestrator/server package adapts
// infra/plugins/kinds.AuthPlugin to the local AuthPlugin shape — this
// keeps the auth feature free of any infra/plugins import (which would
// cycle through io/http/contentloader).
func RegisterAuthPlugins(m map[string]AuthPlugin) {
	authPlugins = map[string]AuthPlugin{}
	for k, v := range m {
		authPlugins[k] = v
	}
}

// pluginAuthTimeout caps the per-attempt RPC time to a value comfortable
// for human-driven flows but short enough that a hung plugin doesn't
// stall the request goroutine forever.
const pluginAuthTimeout = 30 * time.Second

// setupAuthPlugins runs in InitAuthManager next to setupOIDC/setupOAuth.
// It validates that every Type == "plugin" config names a non-empty
// plugin. Actual plugin resolution happens in
// orchestrator/server/auth_plugin.go (which calls RegisterAuthPlugins)
// — that's where infra/plugins/kinds is reachable without a cycle.
func setupAuthPlugins(configs map[string]*AuthConfig) error {
	for name, cfg := range configs {
		if cfg == nil || !strings.EqualFold(cfg.Type, "plugin") {
			continue
		}
		if cfg.Plugin == "" {
			return fmt.Errorf("auth %q: type=plugin requires a `plugin:` field", name)
		}
		log.Printf("[INFO] Auth plugin requested: name=%s plugin=%s", name, cfg.Plugin)
	}
	return nil
}

// pluginAuthenticate translates LoginForm → AuthRequest, calls the
// plugin's Authenticate, and translates AuthResult back into the
// orchestrator's LoginResponse. r may be nil (then headers/cookies are
// empty).
func (am *AuthManager) pluginAuthenticate(form LoginForm, config *AuthConfig, r *http.Request) *LoginResponse {
	plugin, ok := authPlugins[config.key]
	if !ok || plugin == nil {
		return &LoginResponse{
			Success: false,
			Error:   fmt.Sprintf("auth %q: plugin not initialized", config.key),
			Code:    "plugin_not_initialized",
		}
	}

	req := buildPluginAuthRequest(form, r)
	ctx, cancel := context.WithTimeout(context.Background(), pluginAuthTimeout)
	defer cancel()

	result, err := plugin.Authenticate(ctx, req)
	if err != nil {
		return &LoginResponse{
			Success: false,
			Error:   fmt.Sprintf("plugin authenticate: %v", err),
			Code:    "plugin_error",
		}
	}
	if result == nil || !result.Authenticated {
		// Plugin can still want a redirect (e.g. SAML init returns the
		// IdP URL with Authenticated=false) — surface that.
		resp := &LoginResponse{Success: false, Error: "authentication failed", Code: "unauthenticated"}
		if result != nil {
			if result.Redirect != "" {
				resp.RedirectTo = result.Redirect
			}
			if len(result.SetCookies) > 0 {
				resp.ExtraCookies = translatePluginCookies(result.SetCookies)
			}
		}
		return resp
	}

	// Authenticated. Mint local session + JWT using the orchestrator's
	// normal machinery, with the plugin's claims.Subject as the user ID
	// surrogate (kept negative to never collide with DB user IDs).
	user := userFromClaims(result.Claims)
	resp := am.generateLoginResponse(user, config)
	if !resp.Success {
		return resp
	}

	// Plugin-set extras — append our cookies to the orchestrator's.
	if len(result.SetCookies) > 0 {
		resp.ExtraCookies = translatePluginCookies(result.SetCookies)
	}
	// Plugin redirect overrides config.RedirectOnSuccess.
	if result.Redirect != "" {
		resp.RedirectTo = result.Redirect
	}
	return resp
}

// pluginLogout is best-effort: any plugin error is logged and discarded
// so the orchestrator can still clear the local session and cookie.
func (am *AuthManager) pluginLogout(r *http.Request, config *AuthConfig) *LogoutResponse {
	plugin, ok := authPlugins[config.key]
	if !ok || plugin == nil {
		return nil
	}
	subject := ""
	if r != nil {
		if tok, err := am.extractToken(r, config); err == nil {
			if claims, err := jwt.Parse(am.jwtSecret, tok); err == nil {
				subject = claims.Username
			}
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), pluginAuthTimeout)
	defer cancel()
	if err := plugin.Logout(ctx, subject); err != nil {
		log.Printf("[WARN] auth plugin %q logout failed: %v", config.Plugin, err)
	}
	return nil
}

// buildPluginAuthRequest packages a LoginForm and the inbound request
// into the AuthRequest the plugin sees. Method defaults to "password"
// (the only method built-in handlers send today); plugin-aware callers
// can hint other flows by stuffing a "method" field into form.Username
// — explicit method threading is left to a follow-up.
func buildPluginAuthRequest(form LoginForm, r *http.Request) *AuthPluginRequest {
	creds := map[string]string{}
	if form.Username != "" {
		creds["username"] = form.Username
	}
	if form.Password != "" {
		creds["password"] = form.Password
	}

	method := "password"

	headers := map[string]string{}
	cookies := map[string]string{}
	if r != nil {
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
		for _, c := range r.Cookies() {
			cookies[c.Name] = c.Value
		}
		// Allow plugin-aware route handlers to thread a custom method
		// via the X-Auth-Method header without needing a new struct.
		if m := r.Header.Get("X-Auth-Method"); m != "" {
			method = m
		}
	}

	return &AuthPluginRequest{
		Method:      method,
		Credentials: creds,
		Headers:     headers,
		Cookies:     cookies,
	}
}

// translatePluginCookies converts the dependency-free AuthPluginCookie
// shape to the net/http one the response writer needs.
func translatePluginCookies(in []*AuthPluginCookie) []*http.Cookie {
	out := make([]*http.Cookie, 0, len(in))
	for _, c := range in {
		if c == nil {
			continue
		}
		out = append(out, &http.Cookie{
			Name:     c.Name,
			Value:    c.Value,
			Path:     c.Path,
			Domain:   c.Domain,
			MaxAge:   c.MaxAge,
			Secure:   c.Secure,
			HttpOnly: c.HTTPOnly,
			SameSite: parseSameSite(c.SameSite),
		})
	}
	return out
}

func parseSameSite(s string) http.SameSite {
	switch strings.ToLower(s) {
	case "lax":
		return http.SameSiteLaxMode
	case "strict":
		return http.SameSiteStrictMode
	case "none":
		return http.SameSiteNoneMode
	default:
		return http.SameSiteDefaultMode
	}
}

// pluginUserIDCounter assigns negative IDs to plugin-supplied identities
// so they never collide with database-backed user IDs (which start at 1).
// The counter is process-local; sessions don't survive a restart anyway.
var pluginUserIDCounter int = -10_000_000

// userFromClaims synthesizes a *User from plugin Claims. The orchestrator
// currently keys sessions by integer ID; plugin subjects are strings, so
// we hash to a stable-ish negative int per process.
func userFromClaims(c *AuthPluginClaims) *domain.User {
	if c == nil {
		return &domain.User{ID: pluginUserIDCounter, Username: "anonymous", IsDefault: true}
	}
	id := stableNegativeID(c.Subject)
	username := c.Subject
	if c.Email != "" {
		username = c.Email
	}
	return &domain.User{
		ID:        id,
		Username:  username,
		IsDefault: true,
	}
}

// stableNegativeID derives a negative int from a string using FNV-1a.
// Negative so plugin-issued IDs never collide with DB-backed users.
func stableNegativeID(s string) int {
	const offset64 = 14695981039346656037
	const prime64 = 1099511628211
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	v := int(h & 0x7fffffff)
	if v == 0 {
		v = 1
	}
	return -v
}
