// ./wave/auth/setup.go
package auth

import (
	infrajwt "wave/infra/jwt"
	"fmt"
	"net/http"
	"strings"
)

type AuthenticationResponse struct {
	User          *User
	Error         error
	Success       bool
	Location      string
	Name          string
	Value         string
	Message       string
	TokenDuration int
}

func (am *AuthManager) authenticate(r *http.Request, config *AuthConfig) *AuthenticationResponse {
	var user *User
	var err error

	switch config.Type {
	case "jwt":
		user, err = am.authenticateJWT(r, config)
	case "api_key":
		user, err = am.authenticateAPIKey(r, config)
	case "oidc":
		user, err = am.authenticateOIDC(r, config)
	default:
		err = fmt.Errorf("unsupported authentication type: %s", config.Type)
	}

	return &AuthenticationResponse{
		User:          user,
		Error:         err,
		Success:       err == nil && user != nil,
		Location:      config.TokenLocation,
		Name:          config.HeaderName,
		Value:         "",
		TokenDuration: -1,
	}
}

func (am *AuthManager) authenticateJWT(r *http.Request, config *AuthConfig) (*User, error) {
	tokenString, err := am.extractToken(r, config)
	if err != nil {
		return nil, fmt.Errorf("token extraction failed: %w", err)
	}

	claims, err := infrajwt.Parse(am.jwtSecret, tokenString)
	if err != nil {
		return nil, fmt.Errorf("token parse error: %w", err)
	}

	// Validate session
	if sessionStore, ok := am.sessionStores[config.key]; ok {
		_, err := sessionStore.GetSession(claims.SessionID)
		if err != nil {
			return nil, fmt.Errorf("invalid session: %w", err)
		}
	}

	// Check if it's a default user (by negative ID)
	if claims.UserID < 0 {
		// Find default user by username
		if defaultUser, exists := config.defaultUsers[claims.Username]; exists {
			return defaultUser, nil
		}
		return nil, fmt.Errorf("default user not found: %s", claims.Username)
	}

	// Regular database user
	userStore, found := stores[config.key]
	if !found {
		return nil, fmt.Errorf("user store not found: '%s'", config.key)
	}

	user, err := userStore.GetUserByID(claims.UserID)
	if err != nil {
		return nil, fmt.Errorf("user not found: %w", err)
	}

	return user, nil
}

func (am *AuthManager) authenticateAPIKey(r *http.Request, config *AuthConfig) (*User, error) {
	return nil, fmt.Errorf("API key authentication not yet implemented")
}

func (am *AuthManager) extractToken(r *http.Request, config *AuthConfig) (string, error) {
	switch strings.ToLower(config.TokenLocation) {
	case "header":
		return am.extractTokenFromHeader(r, config)
	case "cookie":
		return am.extractTokenFromCookie(r, config)
	case "both", "":
		// Try cookie first, then header
		token, err := am.extractTokenFromCookie(r, config)
		if err == nil {
			return token, nil
		}
		return am.extractTokenFromHeader(r, config)
	default:
		return "", fmt.Errorf("unsupported token location: %s", config.TokenLocation)
	}
}

func (am *AuthManager) extractTokenFromHeader(r *http.Request, config *AuthConfig) (string, error) {
	authHeader := r.Header.Get(config.HeaderName)
	if authHeader == "" {
		return "", fmt.Errorf("missing authorization header: %s", config.HeaderName)
	}

	if config.HeaderScheme != "" {
		prefix := config.HeaderScheme + " "
		if !strings.HasPrefix(authHeader, prefix) {
			return "", fmt.Errorf("invalid authorization header format, expected scheme: %s", config.HeaderScheme)
		}
		return strings.TrimPrefix(authHeader, prefix), nil
	}

	return authHeader, nil
}

func (am *AuthManager) extractTokenFromCookie(r *http.Request, config *AuthConfig) (string, error) {
	cookieName := config.CookieName
	if cookieName == "" {
		cookieName = config.HeaderName
	}

	cookie, err := r.Cookie(cookieName)
	if err != nil {
		return "", fmt.Errorf("missing authorization cookie: %s", cookieName)
	}

	if cookie.Value == "" {
		return "", fmt.Errorf("empty authorization cookie: %s", cookieName)
	}

	return cookie.Value, nil
}