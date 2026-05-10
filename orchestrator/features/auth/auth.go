// ./wave/auth/auth.go
package auth

import (
	"wave/domain"
	"wave/infra/cookies"
	infrajwt "wave/infra/jwt"
	"wave/infra/sessions"
	"net/http"
	"os"
	"strings"
	"time"
)

type User = domain.User
type PublicUser = domain.PublicUser
type Session = domain.Session
type Claims = infrajwt.Claims

type ResponseRenderer func(w http.ResponseWriter, r *http.Request, data interface{}) error

// AuthOAuthConfig is the YAML shape consumed by oauth_bridge.go to
// build a per-AuthConfig oauth.Provider at boot. Field names mirror
// infra/oauth.Config.
type AuthOAuthConfig struct {
	Provider     string   `json:"provider,omitempty" yaml:"provider,omitempty"`
	ClientID     string   `json:"client_id,omitempty" yaml:"client_id,omitempty"`
	ClientSecret string   `json:"client_secret,omitempty" yaml:"client_secret,omitempty"`
	Scopes       []string `json:"scopes,omitempty" yaml:"scopes,omitempty"`

	// Generic provider only.
	AuthorizeURL string `json:"authorize_url,omitempty" yaml:"authorize_url,omitempty"`
	TokenURL     string `json:"token_url,omitempty" yaml:"token_url,omitempty"`
	UserinfoURL  string `json:"userinfo_url,omitempty" yaml:"userinfo_url,omitempty"`

	// Apple-specific.
	AppleTeamID         string `json:"apple_team_id,omitempty" yaml:"apple_team_id,omitempty"`
	AppleKeyID          string `json:"apple_key_id,omitempty" yaml:"apple_key_id,omitempty"`
	ApplePrivateKeyPath string `json:"apple_private_key_path,omitempty" yaml:"apple_private_key_path,omitempty"`
	ApplePrivateKeyPEM  string `json:"apple_private_key_pem,omitempty" yaml:"apple_private_key_pem,omitempty"`
}

type DefaultLogin struct {
	Username string `json:"username" yaml:"username"`
	Password string `json:"password" yaml:"password"`
}

type AuthConfig struct {
	key                  string
	Name                 string            `json:"name" yaml:"name"`
	Type                 string            `json:"type" yaml:"type"`
	TokenLocation        string            `json:"token_location" yaml:"token_location"`
	TokenDurationSeconds int               `json:"token_duration_seconds" yaml:"token_duration_seconds"`
	HeaderName           string            `json:"header_name" yaml:"header_name"`
	HeaderScheme         string            `json:"header_scheme" yaml:"header_scheme"`
	CookieName           string            `json:"cookie_name" yaml:"cookie_name"`
	Secret               string            `json:"secret" yaml:"secret"`
	SessionStore         string            `json:"session_store" yaml:"session_store"`
	UserStore            string            `json:"user_store" yaml:"user_store"`
	RedirectOnFailure    string            `json:"redirect_on_failure" yaml:"redirect_on_failure"`
	RedirectOnSuccess    string            `json:"redirect_on_success" yaml:"redirect_on_success"`
	SecureCookie         *bool             `json:"secure_cookie" yaml:"secure_cookie"`
	CookieSameSite       string            `json:"cookie_same_site" yaml:"cookie_same_site"`
	CookieDomain         string            `json:"cookie_domain" yaml:"cookie_domain"`
	ResponseContentType  string            `json:"response_content_type" yaml:"response_content_type"`
	ResponseTemplate     string            `json:"response_template" yaml:"response_template"`
	ResponseRenderer     ResponseRenderer  `json:"-" yaml:"-"`
	ForwardToEndpoint    string            `json:"forward_to_endpoint" yaml:"forward_to_endpoint"`
	Params               map[string]string `json:"params" yaml:"params"`
	DefaultLogins        []DefaultLogin    `json:"default_logins" yaml:"default_logins"`

	// OIDC-specific fields. Populated when Type == "oidc". The verifier
	// itself is built once at boot and stashed in the package-level
	// oidcVerifiers map (see oidc_bridge.go).
	Issuer   string `json:"issuer,omitempty" yaml:"issuer,omitempty"`
	ClientID string `json:"client_id,omitempty" yaml:"client_id,omitempty"`

	// OAuth-specific fields. Populated when Type == "oauth". The
	// resolved oauth.Provider is cached at boot in oauth_bridge.go.
	OAuth *AuthOAuthConfig `json:"oauth,omitempty" yaml:"oauth,omitempty"`

	// Plugin-specific fields. Populated when Type == "plugin".
	// Names a plugin in the top-level `plugins:` map (kind: auth).
	// The orchestrator continues to own sessions, cookies, and JWTs;
	// the plugin only provides identity (Authenticate / RefreshClaims /
	// Logout). Resolved at boot in plugin_bridge.go.
	Plugin string `json:"plugin,omitempty" yaml:"plugin,omitempty"`

	// In-memory default user store
	defaultUsers map[string]*User `json:"-" yaml:"-"`
}

// createAuthCookie builds an HTTP cookie using the configured cookie
// policy from AuthConfig. The actual same-site / secure / domain logic
// lives in infra/cookies.
func createAuthCookie(name, value string, config *AuthConfig, r *http.Request, maxAge int) *http.Cookie {
	return cookies.Build(name, value, cookies.Policy{
		Secure:      config.SecureCookie,
		SameSiteRaw: config.CookieSameSite,
		Domain:      config.CookieDomain,
	}, r, maxAge)
}

type UserStore interface {
	GetUserByID(id int) (*User, error)
	GetUserByUsername(username string) (*User, error)
	ValidatePassword(username, password string) error
	CreateUser(username string, hashedPassword []byte) (*User, error)
	UserExists(username string) (bool, error)
}

type SessionStore interface {
	CreateSession(userID string, duration time.Duration) (*Session, error)
	GetSession(sessionID string) (*Session, error)
	RevokeSession(sessionID string) error
}

type AuthManager struct {
	configs       map[string]*AuthConfig
	jwtSecret     []byte
	sessionStores map[string]SessionStore
}

type LoginResponse struct {
	Success       bool              `json:"success"`
	Location      string            `json:"location,omitempty"`
	Error         string            `json:"error,omitempty"`
	Code          string            `json:"code,omitempty"`
	Details       map[string]string `json:"details,omitempty"`
	Name          string            `json:"name,omitempty"`
	Value         string            `json:"value,omitempty"`
	Message       string            `json:"message,omitempty"`
	TokenDuration int               `json:"token_duration,omitempty"`
	User          *PublicUser       `json:"user,omitempty"`
	RedirectTo    string            `json:"redirect_to,omitempty"`
	// ExtraCookies are appended verbatim by plugin-backed auth flows
	// (e.g. SAML RelayState). The orchestrator-owned auth cookie is
	// still emitted via Name/Value; ExtraCookies are written *in
	// addition* to it.
	ExtraCookies []*http.Cookie `json:"-"`
}

type LogoutResponse struct {
	Success    bool   `json:"success"`
	Location   string `json:"location,omitempty"`
	Name       string `json:"name,omitempty"`
	Value      string `json:"value,omitempty"`
	Message    string `json:"message,omitempty"`
	Error      string `json:"error,omitempty"`
	Code       string `json:"code,omitempty"`
	RedirectTo string `json:"redirect_to,omitempty"`
}

type LoginForm struct {
	Username string
	Password string
}

type SignupForm struct {
	Username       string
	Password       string
	PasswordRepeat string
}

var stores = map[string]UserStore{}

type contextKey string

const (
	UserContextKey     contextKey = "auth_user"
	AuthResponseKey    contextKey = "auth_response"
	AuthErrorKey       contextKey = "auth_error"
	AuthSuccessKey     contextKey = "auth_success"
	AuthRedirectKey    contextKey = "auth_redirect"
	AuthDataKey        contextKey = "auth_data"
	AuthConfigKey      contextKey = "auth_config"
	OriginalRequestKey contextKey = "original_request"
)

func IsBrowserRequest(r *http.Request) bool {
	acceptHeader := r.Header.Get("Accept")
	userAgent := r.Header.Get("User-Agent")

	return strings.Contains(acceptHeader, "text/html") ||
		strings.Contains(userAgent, "Mozilla")
}

const StorageDir = "./db_storage"

// InMemorySessionStore lives in infra/sessions; this alias keeps the
// legacy auth.InMemorySessionStore name available to existing callers.
type InMemorySessionStore = sessions.InMemoryStore

func NewInMemorySessionStore() *sessions.InMemoryStore {
	return sessions.NewInMemoryStore()
}

// Global auth manager instance
var authManager *AuthManager

func InitAuthManager(authConfig map[string]*AuthConfig) error {
	jwtSecret := os.Getenv("SECRET_KEY")
	var err error
	authManager, err = NewAuthManager(authConfig, jwtSecret)
	if err != nil {
		return err
	}
	// Build OIDC verifiers (one per `type: oidc` entry). Boot-time
	// errors fail fast — the IdP is unreachable / misconfigured.
	if err := setupOIDC(authConfig); err != nil {
		return err
	}
	// Build OAuth providers (one per `type: oauth` entry).
	if err := setupOAuth(authConfig); err != nil {
		return err
	}
	// Resolve auth-kind plugins (one per `type: plugin` entry).
	if err := setupAuthPlugins(authConfig); err != nil {
		return err
	}
	return nil
}
func ensureAuthManagerIsInitialized() {
	if authManager == nil {
		panic("Must init auth manager")
	}
}

func RequireAuth(next http.Handler, authConfigNames ...string) http.Handler {
	ensureAuthManagerIsInitialized()
	return authManager.RequireAuth(next, authConfigNames...)
}

func ValidateSignIn(r *http.Request) (string, error) {
	ensureAuthManagerIsInitialized()
	return authManager.validateSignIn(r)
}

func Login(form LoginForm, auth string) *LoginResponse {
	ensureAuthManagerIsInitialized()
	return authManager.Login(form, auth)
}

// LoginWithRequest is the request-aware variant. Only plugin-backed auth
// (Type == "plugin") needs the underlying *http.Request to thread headers
// and cookies through to the plugin; for everything else this is
// equivalent to Login.
func LoginWithRequest(form LoginForm, auth string, r *http.Request) *LoginResponse {
	ensureAuthManagerIsInitialized()
	return authManager.LoginWithRequest(form, auth, r)
}

func Signup(form SignupForm, auth string) *LoginResponse {
	ensureAuthManagerIsInitialized()
	return authManager.Signup(form, auth)
}

func Logout(r *http.Request, auth string) *LogoutResponse {
	ensureAuthManagerIsInitialized()
	return authManager.Logout(r, auth)
}

func GenerateJWT(user *User, sessionID string, expiry time.Duration) (string, error) {
	ensureAuthManagerIsInitialized()
	return authManager.GenerateJWT(user, sessionID, expiry)
}

func CreateSession(userID string, duration time.Duration) (string, error) {
	ensureAuthManagerIsInitialized()
	return authManager.createSession(userID, duration)
}
