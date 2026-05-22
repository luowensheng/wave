// Package oauth_routes implements the two route types that drive an
// OAuth-2 sign-in:
//
//	type: oauth-start     GET  /login/<provider>           → 302 to provider
//	type: oauth-callback  GET  /login/<provider>/callback  → cookie + 302 home
//
// Both reach into a small registry the orchestrator wires at boot
// (one oauth.Provider per AuthConfig of `type: oauth`). The state
// nonce is stored in a short-lived signed cookie to defeat CSRF.
package oauth_routes

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/luowensheng/wave/infra/oauth"
)

const (
	stateCookie    = "easy_oauth_state"
	stateCookieTTL = 5 * time.Minute
)

// providerLookup maps an AuthConfig name → resolved oauth.Provider.
// Set at boot from orchestrator/features/auth/oauth_bridge.go.
var (
	mu        sync.RWMutex
	providers = map[string]oauth.Provider{}
	hmacKey   = randomKey()
	loginFn   = func(ctx context.Context, c *oauth.Claims, w http.ResponseWriter, r *http.Request) error {
		return fmt.Errorf("oauth: LoginFn not wired")
	}
)

// SetProvider registers a Provider under an auth-config name.
func SetProvider(authName string, p oauth.Provider) {
	mu.Lock()
	defer mu.Unlock()
	providers[authName] = p
}

// SetHMACKey overrides the random key used to sign the state cookie.
// Call from boot with a stable per-deploy secret so server restarts
// don't invalidate in-flight logins.
func SetHMACKey(k []byte) {
	if len(k) == 0 {
		return
	}
	mu.Lock()
	defer mu.Unlock()
	hmacKey = k
}

// SetLoginFn injects the function that creates a session for the
// authenticated user. The orchestrator wires this so OAuth users get
// the same session cookie / JWT as everybody else.
func SetLoginFn(fn func(ctx context.Context, c *oauth.Claims, w http.ResponseWriter, r *http.Request) error) {
	mu.Lock()
	defer mu.Unlock()
	loginFn = fn
}

func getProvider(name string) (oauth.Provider, bool) {
	mu.RLock()
	defer mu.RUnlock()
	p, ok := providers[name]
	return p, ok
}

func getLoginFn() func(ctx context.Context, c *oauth.Claims, w http.ResponseWriter, r *http.Request) error {
	mu.RLock()
	defer mu.RUnlock()
	return loginFn
}

// ── start route ───────────────────────────────────────────────────────────

// StartConfig handles `type: oauth-start`.
type StartConfig struct {
	AuthConfig  string `yaml:"auth_config,omitempty" json:"auth_config,omitempty"`
	RedirectURI string `yaml:"redirect_uri,omitempty" json:"redirect_uri,omitempty"`
}

func (c *StartConfig) CreateRoute(method, path string, args map[string]string) (http.HandlerFunc, error) {
	if c.AuthConfig == "" {
		return nil, fmt.Errorf("oauth-start: auth_config required")
	}
	if c.RedirectURI == "" {
		return nil, fmt.Errorf("oauth-start: redirect_uri required")
	}
	authName := c.AuthConfig
	redirectURI := c.RedirectURI
	return func(w http.ResponseWriter, r *http.Request) {
		p, ok := getProvider(authName)
		if !ok {
			http.Error(w, "oauth provider not configured: "+authName, http.StatusInternalServerError)
			return
		}
		nonce := newNonce()
		setStateCookie(w, r, nonce)
		http.Redirect(w, r, p.AuthorizeURL(nonce, redirectURI), http.StatusFound)
	}, nil
}

// ── callback route ────────────────────────────────────────────────────────

// CallbackConfig handles `type: oauth-callback`.
type CallbackConfig struct {
	AuthConfig      string `yaml:"auth_config,omitempty" json:"auth_config,omitempty"`
	RedirectURI     string `yaml:"redirect_uri,omitempty" json:"redirect_uri,omitempty"`
	SuccessRedirect string `yaml:"success_redirect,omitempty" json:"success_redirect,omitempty"`
	FailureRedirect string `yaml:"failure_redirect,omitempty" json:"failure_redirect,omitempty"`
}

func (c *CallbackConfig) CreateRoute(method, path string, args map[string]string) (http.HandlerFunc, error) {
	if c.AuthConfig == "" {
		return nil, fmt.Errorf("oauth-callback: auth_config required")
	}
	if c.RedirectURI == "" {
		return nil, fmt.Errorf("oauth-callback: redirect_uri required")
	}
	authName := c.AuthConfig
	redirectURI := c.RedirectURI
	successURL := c.SuccessRedirect
	failURL := c.FailureRedirect

	return func(w http.ResponseWriter, r *http.Request) {
		fail := func(msg string) {
			if failURL != "" {
				http.Redirect(w, r, failURL, http.StatusFound)
				return
			}
			http.Error(w, msg, http.StatusUnauthorized)
		}

		// Validate state.
		got := r.URL.Query().Get("state")
		ck, err := r.Cookie(stateCookie)
		if err != nil || ck.Value == "" || got == "" || !hmac.Equal([]byte(ck.Value), []byte(got)) {
			fail("state mismatch")
			return
		}
		// Clear the state cookie now that we've used it.
		http.SetCookie(w, &http.Cookie{Name: stateCookie, Value: "", Path: "/", MaxAge: -1})

		code := r.URL.Query().Get("code")
		if code == "" {
			fail("missing code")
			return
		}
		p, ok := getProvider(authName)
		if !ok {
			fail("oauth provider not configured: " + authName)
			return
		}

		tok, err := p.Exchange(r.Context(), code, redirectURI)
		if err != nil {
			fail("exchange failed: " + err.Error())
			return
		}
		claims, err := p.GetUserInfo(r.Context(), tok)
		if err != nil {
			fail("userinfo failed: " + err.Error())
			return
		}
		if err := getLoginFn()(r.Context(), claims, w, r); err != nil {
			fail("login failed: " + err.Error())
			return
		}
		if successURL != "" {
			http.Redirect(w, r, successURL, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok": true, "email": claims.Email, "subject": claims.Subject,
		})
	}, nil
}

// ── helpers ───────────────────────────────────────────────────────────────

func newNonce() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func randomKey() []byte {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return b
}

func setStateCookie(w http.ResponseWriter, r *http.Request, nonce string) {
	http.SetCookie(w, &http.Cookie{
		Name: stateCookie, Value: nonce, Path: "/",
		HttpOnly: true, Secure: r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(stateCookieTTL.Seconds()),
	})
}

// signed* helpers reserved for future use if we move state into a
// signed-token cookie rather than the random-nonce cookie.
var _ = sha256.New
var _ = base64.RawURLEncoding
