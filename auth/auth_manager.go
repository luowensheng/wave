// ./easyserver/auth/auth_manager.go
package auth

import (
	"context"
	"easyserver/pkg/common"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

func NewAuthManager(configs map[string]*AuthConfig, jwtSecret string) (*AuthManager, error) {
	if jwtSecret == "" {
		return nil, fmt.Errorf("JWT secret cannot be empty")
	}

	authManager = &AuthManager{
		configs:       configs,
		jwtSecret:     []byte(jwtSecret),
		sessionStores: make(map[string]SessionStore),
	}

	for key, config := range configs {
		if config.Type == "none" || config.Type == "" {
			continue
		}

		config.key = key

		// Set defaults
		if config.HeaderName == "" {
			config.HeaderName = "Authorization"
		}
		if config.CookieName == "" {
			config.CookieName = "auth_token"
		}
		if config.TokenLocation == "" {
			config.TokenLocation = "cookie"
		}

		// Initialize default users map
		config.defaultUsers = make(map[string]*User)

		// Load default logins into memory (not stored in DB)
		if len(config.DefaultLogins) > 0 {
			for i, login := range config.DefaultLogins {
				password := login.Password
				if strings.HasPrefix(password, "$") {
					password = os.Getenv(password[1:])
				}

				if password == "" {
					log.Printf("[WARNING] Empty password for default user: %s", login.Username)
					continue
				}

				hashedPassword, err := bcrypt.GenerateFromPassword(
					[]byte(password),
					bcrypt.DefaultCost,
				)
				if err != nil {
					log.Printf("[ERROR] Failed to hash password for default user %s: %v", login.Username, err)
					continue
				}

				// Create default user in memory with negative IDs to avoid conflicts
				user := &User{
					ID:        -(i + 1), // Negative IDs for default users
					Username:  login.Username,
					Password:  hashedPassword,
					CreatedAt: time.Now(),
					IsDefault: true,
				}

				config.defaultUsers[login.Username] = user
				log.Printf("[INFO] Loaded default user: %s (ID: %d)", login.Username, user.ID)
			}
		}

		// Initialize user store
		var store UserStore
		var err error

		switch config.UserStore {
		case "sqlite":
			path := ""
			if config.Params != nil {
				path = strings.TrimSpace(config.Params["db_path"])
			}
			if path == "" {
				os.MkdirAll(StorageDir, 0755)
				path = filepath.Join(StorageDir, fmt.Sprintf("%s_storage.db", key))
			}
			store, err = NewSQLiteUserStore(path)
			if err != nil {
				return nil, fmt.Errorf("failed to create SQLite store for %s: %w", key, err)
			}
		case "memory", "":
			store = NewInMemoryUserStore()
		default:
			return nil, fmt.Errorf("invalid user store type: %s", config.UserStore)
		}

		stores[key] = store
		authManager.sessionStores[key] = NewInMemorySessionStore()
	}

	return authManager, nil
}

func (am *AuthManager) RequireAuth(next http.Handler, authConfigNames ...string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(authConfigNames) == 0 {
			http.Error(w, "No auth configurations specified", http.StatusInternalServerError)
			return
		}

		var lastError error
		var lastConfig *AuthConfig

		for _, authConfigName := range authConfigNames {
			config, exists := am.configs[authConfigName]
			if !exists {
				http.Error(w, fmt.Sprintf("Auth configuration '%s' not found", authConfigName), http.StatusInternalServerError)
				return
			}

			lastConfig = config

			// Handle public routes
			if config.Type == "" || config.Type == "none" {
				redirectURL, err := am.validateSignIn(r)
				if err == nil && redirectURL != "" {
					http.Redirect(w, r, redirectURL, http.StatusFound)
					return
				}
				next.ServeHTTP(w, r)
				return
			}

			// Attempt authentication
			result := am.authenticate(r, config)
			if result.Success && result.User != nil {
				// Authentication successful
				user := &PublicUser{
					Username: result.User.Username,
					ID:       result.User.ID,
				}
				ctx := context.WithValue(r.Context(), UserContextKey, user)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}

			lastError = result.Error
		}

		// All authentication attempts failed
		am.handleAuthFailure(w, r, lastConfig, lastError)
	})
}

func (am *AuthManager) handleAuthFailure(w http.ResponseWriter, r *http.Request, config *AuthConfig, err error) {
	// Clear any invalid cookies
	if config != nil {
		cookieName := config.CookieName
		if cookieName == "" {
			cookieName = config.HeaderName
		}

		clearCookie := createAuthCookie(cookieName, "", config, r, -1)
		http.SetCookie(w, clearCookie)
	}

	message := "Unauthorized"
	if err != nil {
		message = err.Error()
	}

	// For browser GET requests, redirect to login page
	if config != nil && config.RedirectOnFailure != "" &&
		IsBrowserRequest(r) && r.Method == http.MethodGet {
		http.Redirect(w, r, config.RedirectOnFailure, http.StatusFound)
		return
	}

	// For all other requests (POST, API calls, etc), return JSON error
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	json.NewEncoder(w).Encode(map[string]interface{}{
		"success": false,
		"error":   message,
		"code":    "unauthorized",
	})
}

func (am *AuthManager) validateSignIn(r *http.Request) (string, error) {
	for _, config := range am.configs {
		result := am.authenticate(r, config)
		if result.Success && config.RedirectOnSuccess != "" {
			return config.RedirectOnSuccess, nil
		}
	}
	return "", fmt.Errorf("user is not signed in")
}

func (am *AuthManager) Login(form LoginForm, auth string) *LoginResponse {
	config, found := am.configs[auth]
	if !found {
		return &LoginResponse{
			Success: false,
			Error:   fmt.Sprintf("auth config not found: %s", auth),
			Code:    "config_not_found",
		}
	}

	if form.Username == "" || form.Password == "" {
		return &LoginResponse{
			Success: false,
			Error:   "Username and password required",
			Code:    "missing_credentials",
			Details: map[string]string{
				"username": func() string {
					if form.Username == "" {
						return "required"
					}
					return "ok"
				}(),
				"password": func() string {
					if form.Password == "" {
						return "required"
					}
					return "ok"
				}(),
			},
		}
	}

	// Check default users first (in-memory, not in DB)
	if defaultUser, exists := config.defaultUsers[form.Username]; exists {
		err := bcrypt.CompareHashAndPassword(defaultUser.Password, []byte(form.Password))
		if err == nil {
			// Valid default user login
			return am.generateLoginResponse(defaultUser, config)
		}
	}

	// Check database users
	store, found := stores[auth]
	if !found {
		return &LoginResponse{
			Success: false,
			Error:   fmt.Sprintf("auth store not found: %s", auth),
			Code:    "store_not_found",
		}
	}

	err := store.ValidatePassword(form.Username, form.Password)
	if err != nil {
		return &LoginResponse{
			Success: false,
			Error:   "Invalid credentials",
			Code:    "invalid_credentials",
		}
	}

	user, err := store.GetUserByUsername(form.Username)
	if err != nil {
		return &LoginResponse{
			Success: false,
			Error:   "User not found",
			Code:    "user_not_found",
		}
	}

	return am.generateLoginResponse(user, config)
}

func (am *AuthManager) generateLoginResponse(user *User, config *AuthConfig) *LoginResponse {
	sessionDuration := 86400 // 24 hours default
	if config.TokenDurationSeconds > 0 {
		sessionDuration = config.TokenDurationSeconds
	}

	sessionID, err := am.createSession(fmt.Sprint(user.ID), time.Duration(sessionDuration)*time.Second)
	if err != nil {
		return &LoginResponse{
			Success: false,
			Error:   "Failed to create session",
			Code:    "session_creation_failed",
		}
	}

	token, err := am.GenerateJWT(user, sessionID, time.Duration(sessionDuration)*time.Second)
	if err != nil {
		return &LoginResponse{
			Success: false,
			Error:   "Failed to generate token",
			Code:    "token_generation_failed",
		}
	}

	cookieName := config.CookieName
	if cookieName == "" {
		cookieName = config.HeaderName
	}

	return &LoginResponse{
		Success:       true,
		Message:       "Login successful",
		Location:      config.TokenLocation,
		Name:          cookieName,
		Value:         token,
		TokenDuration: sessionDuration,
		User: &PublicUser{
			ID:       user.ID,
			Username: user.Username,
		},
		RedirectTo: config.RedirectOnSuccess,
	}
}

func (am *AuthManager) Signup(form SignupForm, auth string) *LoginResponse {
	common.PrintJSON(common.Object{"form": form})
	config, found := am.configs[auth]
	if !found {
		return &LoginResponse{
			Success: false,
			Error:   fmt.Sprintf("auth config not found: %s", auth),
			Code:    "config_not_found",
		}
	}

	if form.Username == "" || form.Password == "" {
		return &LoginResponse{
			Success: false,
			Error:   "Username and password required",
			Code:    "missing_fields",
			Details: map[string]string{
				"username": func() string {
					if form.Username == "" {
						return "required"
					}
					return "ok"
				}(),
				"password": func() string {
					if form.Password == "" {
						return "required"
					}
					return "ok"
				}(),
			},
		}
	}

	// Check if username conflicts with default users
	if _, exists := config.defaultUsers[form.Username]; exists {
		return &LoginResponse{
			Success: false,
			Error:   "Username already exists",
			Code:    "username_taken",
		}
	}

	if form.Password != form.PasswordRepeat {
		return &LoginResponse{
			Success: false,
			Error:   "Passwords do not match",
			Code:    "password_mismatch",
		}
	}

	if len(form.Password) < 8 {
		return &LoginResponse{
			Success: false,
			Error:   "Password must be at least 8 characters",
			Code:    "password_too_short",
			Details: map[string]string{
				"min_length": "8",
				"provided":   fmt.Sprintf("%d", len(form.Password)),
			},
		}
	}

	store, found := stores[auth]
	if !found {
		return &LoginResponse{
			Success: false,
			Error:   fmt.Sprintf("auth store not found: %s", auth),
			Code:    "store_not_found",
		}
	}

	exists, err := store.UserExists(form.Username)
	if err != nil {
		return &LoginResponse{
			Success: false,
			Error:   "Failed to check user existence",
			Code:    "database_error",
		}
	}
	if exists {
		return &LoginResponse{
			Success: false,
			Error:   "Username already exists",
			Code:    "username_taken",
		}
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(form.Password), bcrypt.DefaultCost)
	if err != nil {
		return &LoginResponse{
			Success: false,
			Error:   "Failed to hash password",
			Code:    "hash_error",
		}
	}

	user, err := store.CreateUser(form.Username, hashedPassword)
	if err != nil {
		return &LoginResponse{
			Success: false,
			Error:   err.Error(),
			Code:    "creation_failed",
		}
	}

	return &LoginResponse{
		Success: true,
		Message: "User created successfully",
		User: &PublicUser{
			ID:       user.ID,
			Username: user.Username,
		},
	}
}

func (am *AuthManager) Logout(r *http.Request, auth string) *LogoutResponse {
	config, found := am.configs[auth]
	if !found {
		return &LogoutResponse{
			Success: false,
			Error:   fmt.Sprintf("Config not found: %s", auth),
			Code:    "config_not_found",
		}
	}

	tokenString, err := am.extractToken(r, config)
	if err != nil {
		return &LogoutResponse{
			Success: false,
			Error:   "No active session",
			Code:    "no_session",
		}
	}

	claims := &Claims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (interface{}, error) {
		if _, ok := token.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, fmt.Errorf("unexpected signing method")
		}
		return am.jwtSecret, nil
	})

	if err == nil && token.Valid && claims.SessionID != "" {
		// Revoke the session
		if sessionStore, ok := am.sessionStores[auth]; ok {
			sessionStore.RevokeSession(claims.SessionID)
		}
	}

	cookieName := config.CookieName
	if cookieName == "" {
		cookieName = config.HeaderName
	}

	return &LogoutResponse{
		Success:    true,
		Message:    "Logout successful",
		Location:   config.TokenLocation,
		Name:       cookieName,
		Value:      "",
		RedirectTo: config.RedirectOnFailure, // Redirect to login after logout
	}
}

func (am *AuthManager) GenerateJWT(user *User, sessionID string, expiry time.Duration) (string, error) {
	now := time.Now()
	claims := &Claims{
		UserID:    user.ID,
		Username:  user.Username,
		SessionID: sessionID,
		Time:      jwt.NewNumericDate(now),
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(now.Add(expiry)),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			Subject:   user.Username,
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString(am.jwtSecret)
}

func (am *AuthManager) createSession(userID string, duration time.Duration) (string, error) {
	if len(am.sessionStores) == 0 {
		return "", fmt.Errorf("no session stores configured")
	}

	for _, store := range am.sessionStores {
		session, err := store.CreateSession(userID, duration)
		if err != nil {
			return "", err
		}
		return session.ID, nil
	}

	return "", fmt.Errorf("failed to create session")
}
