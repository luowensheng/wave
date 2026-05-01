// ./easyserver/auth/auth.go
package auth

import (
	"easyserver/domain"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type User = domain.User
type PublicUser = domain.PublicUser
type Session = domain.Session

type ResponseRenderer func(w http.ResponseWriter, r *http.Request, data interface{}) error

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

	// In-memory default user store
	defaultUsers map[string]*User `json:"-" yaml:"-"`
}

type Claims struct {
	UserID    int              `json:"user_id"`
	Username  string           `json:"username"`
	Time      *jwt.NumericDate `json:"time"`
	SessionID string           `json:"session_id"`
	jwt.RegisteredClaims
}

var serverStartupTime = time.Now()

func (c *Claims) Valid() error {
	now := time.Now()

	if c.Time == nil || c.Time.Time.Before(serverStartupTime) {
		return errors.New("token issued before server startup")
	}

	if c.IssuedAt != nil && c.IssuedAt.Time.Before(serverStartupTime) {
		return errors.New("token issued before server startup")
	}

	if c.ExpiresAt != nil && now.After(c.ExpiresAt.Time) {
		return fmt.Errorf("token has expired: %s", c.ExpiresAt.Time.Format(time.DateTime))
	}

	if c.NotBefore != nil && now.Before(c.NotBefore.Time) {
		return errors.New("token used before valid")
	}

	if c.UserID <= 0 {
		return errors.New("invalid user ID")
	}

	if c.Username == "" {
		return errors.New("username is required")
	}

	if c.SessionID == "" {
		return errors.New("session ID is required")
	}

	return nil
}

func createAuthCookie(name, value string, config *AuthConfig, r *http.Request, maxAge int) *http.Cookie {
	isSecure := isSecureRequest(r)

	if config.SecureCookie != nil {
		isSecure = *config.SecureCookie
	}

	sameSite := getSameSitePolicy(config.CookieSameSite, isSecure)
	if sameSite == http.SameSiteNoneMode {
		isSecure = true
	}

	cookie := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: sameSite,
		MaxAge:   maxAge,
	}

	if config.CookieDomain != "" {
		cookie.Domain = config.CookieDomain
	}

	return cookie
}

func isSecureRequest(r *http.Request) bool {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		return true
	}
	if r.TLS != nil {
		return true
	}
	if r.URL.Scheme == "https" {
		return true
	}
	return false
}

func getSameSitePolicy(configValue string, isSecure bool) http.SameSite {
	switch configValue {
	case "None":
		return http.SameSiteNoneMode
	case "Lax", "": // Lax is the default and recommended for auth cookies
		return http.SameSiteLaxMode
	case "Strict":
		return http.SameSiteStrictMode
	default:
		// Default to Lax for secure connections (better for redirects)
		if isSecure {
			return http.SameSiteLaxMode
		}
		return http.SameSiteDefaultMode
	}
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

type InMemorySessionStore struct {
	data    map[string]*Session
	counter int
	lock    sync.RWMutex
}

func (i *InMemorySessionStore) CreateSession(userID string, duration time.Duration) (*Session, error) {
	i.lock.Lock()
	defer i.lock.Unlock()

	now := time.Now()
	session := &Session{
		ID:        fmt.Sprintf("session_%d_%d", i.counter, now.Unix()),
		UserID:    userID,
		CreatedAt: now,
		ExpiresAt: now.Add(duration),
		Revoked:   false,
	}
	i.counter++
	i.data[session.ID] = session
	return session, nil
}

func (i *InMemorySessionStore) GetSession(sessionID string) (*Session, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()

	session, exists := i.data[sessionID]
	if !exists {
		return nil, errors.New("session not found")
	}

	if session.Revoked {
		return nil, errors.New("session revoked")
	}

	if time.Now().After(session.ExpiresAt) {
		return nil, errors.New("session expired")
	}

	return session, nil
}

func (i *InMemorySessionStore) RevokeSession(sessionID string) error {
	i.lock.Lock()
	defer i.lock.Unlock()

	session, exists := i.data[sessionID]
	if !exists {
		return errors.New("session not found")
	}

	session.Revoked = true
	return nil
}

func NewInMemorySessionStore() *InMemorySessionStore {
	return &InMemorySessionStore{
		data: make(map[string]*Session),
		lock: sync.RWMutex{},
	}
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
