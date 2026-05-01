package servers

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"os"
	"sync"
	"time"
)

// Thread-safe token storage
type CSRFStore struct {
	tokens map[string]time.Time
	mutex  sync.RWMutex
}

var csrfStore = &CSRFStore{
	tokens: make(map[string]time.Time),
}

const tokenDuration = 1 * time.Hour // 1 hour token lifetime

// CSRF Middleware
func (s *Server) csrfMiddleware(next http.Handler) http.HandlerFunc {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Validate CSRF token
		if err := s.ValidateCSRFToken(w, r); err != nil {
			// Error already handled in ValidateCSRFToken
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Generate a secure CSRF token
func generateCSRFToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(bytes), nil
}

// Helper function to validate CSRF token (Double Submit Cookie pattern)
func (s *Server) ValidateCSRFToken(w http.ResponseWriter, r *http.Request) error {
	// Skip CSRF for safe methods
	if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
		return nil
	}

	// Get CSRF token from cookie (automatically sent by browser)
	csrfCookie, err := r.Cookie("csrf_token")
	if err != nil {
		r.Header.Write(os.Stdout)
		http.Error(w, "CSRF token missing from cookie", http.StatusForbidden)
		return fmt.Errorf("CSRF token missing from cookie")
	}

	requestToken := csrfCookie.Value

	if requestToken == "" {
		http.Error(w, "CSRF token missing from request", http.StatusForbidden)
		return fmt.Errorf("CSRF token missing from request")
	}

	// Double submit validation: cookie token must match request token
	if csrfCookie.Value != requestToken {
		http.Error(w, "CSRF token mismatch", http.StatusForbidden)
		return fmt.Errorf("CSRF token mismatch")
	}

	// Validate token exists and isn't expired
	csrfStore.mutex.RLock()
	tokentime, found := csrfStore.tokens[requestToken]
	csrfStore.mutex.RUnlock()

	if !found {
		http.Error(w, "Invalid CSRF token", http.StatusForbidden)
		return fmt.Errorf("invalid CSRF token")
	}

	// Check if token is expired
	if time.Since(tokentime) >= tokenDuration {
		// Clean up expired token
		csrfStore.mutex.Lock()
		delete(csrfStore.tokens, requestToken)
		csrfStore.mutex.Unlock()

		http.Error(w, "CSRF token expired", http.StatusForbidden)
		return fmt.Errorf("CSRF token expired")
	}

	// Tokens should be one-time use for better security
	csrfStore.mutex.Lock()
	delete(csrfStore.tokens, requestToken)
	csrfStore.mutex.Unlock()

	return nil
}

// Helper function to include CSRF token in responses
func (s *Server) IncludeCSRFToken(w http.ResponseWriter, r *http.Request) string {
	// Check if valid token already exists
	if csrfCookie, err := r.Cookie("csrf_token"); err == nil {
		csrfStore.mutex.RLock()
		_, found := csrfStore.tokens[csrfCookie.Value]
		csrfStore.mutex.RUnlock()

		if found {
			return csrfCookie.Value
		}
	}

	// Generate new token
	token, err := generateCSRFToken()
	if err != nil {
		return ""
	}

	// Store token with timestamp
	csrfStore.mutex.Lock()
	csrfStore.tokens[token] = time.Now()
	csrfStore.mutex.Unlock()

	// Set cookie
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token",
		Value:    token,
		Path:     "/",
		HttpOnly: true, // Allow JavaScript to read for header inclusion
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(tokenDuration.Seconds()),
	})

	return token
}

// Cleanup expired tokens periodically
func (s *Server) CleanupExpiredTokens() {
	csrfStore.mutex.Lock()
	defer csrfStore.mutex.Unlock()

	now := time.Now()
	for token, created := range csrfStore.tokens {
		if now.Sub(created) >= tokenDuration {
			delete(csrfStore.tokens, token)
		}
	}
}

// Run cleanup periodically (call this in a goroutine)
func (s *Server) StartTokenCleanup() {
	ticker := time.NewTicker(10 * time.Minute)
	go func() {
		for range ticker.C {
			s.CleanupExpiredTokens()
		}
	}()
}

// // CSRF Middleware
// func (s *Server) csrfMiddleware(next http.Handler) http.HandlerFunc {
// 	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
// 		// Skip CSRF for safe methods (double-check)
// 		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
// 			next.ServeHTTP(w, r)
// 			return
// 		}

// 		// Get CSRF token from cookie
// 		csrfCookie, err := r.Cookie("csrf_token")
// 		if err != nil {
// 			http.Error(w, "CSRF token missing", http.StatusForbidden)
// 			return
// 		}

// 		// Get token from header or form
// 		var requestToken string
// 		if r.Header.Get("X-CSRF-Token") != "" {
// 			requestToken = r.Header.Get("X-CSRF-Token")
// 		} else {
// 			requestToken = r.FormValue("csrf_token")
// 		}

// 		// Validate token
// 		if !secureCompare(csrfCookie.Value, requestToken) {
// 			http.Error(w, "Invalid CSRF token", http.StatusForbidden)
// 			return
// 		}

// 		next.ServeHTTP(w, r)
// 	})
// }

// func secureCompare(a, b string) bool {
// 	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
// }

// var tokenDuration = time.Hour
// var tokenMapping = map[string]time.Time{}

// // Helper function to include CSRF token in responses
// func (s *Server) ValidateCSRFToken(w http.ResponseWriter, r *http.Request) error {
// 	// Check if token already exists
// 	// Skip CSRF for safe methods (double-check)
// 	if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" {
// 		return nil
// 	}

// 	// Get CSRF token from cookie
// 	csrfCookie, err := r.Cookie("csrf_token")
// 	if err != nil {
// 		// http.Error(w, "CSRF token missing", http.StatusForbidden)
// 		return err
// 	}

// 	tokentime, found := tokenMapping[csrfCookie.Value]
// 	if !found {
// 		return fmt.Errorf("missing 'csrf' token")
// 	}

// 	if time.Since(tokentime) >= tokenDuration {
// 		return fmt.Errorf("took too long")

// 	}

// 	return nil
// }

// // Helper function to include CSRF token in responses
// func (s *Server) IncludeCSRFToken(w http.ResponseWriter, r *http.Request) string {
// 	// Check if token already exists
// 	if csrfCookie, err := r.Cookie("csrf_token"); err == nil {
// 		return csrfCookie.Value
// 	}

// 	// Generate new token
// 	token, err := generateCSRFToken()
// 	if err != nil {
// 		return ""
// 	}

// 	tokenMapping[token] = time.Now()

// 	// Set cookie
// 	http.SetCookie(w, &http.Cookie{
// 		Name:     "csrf_token",
// 		Value:    token,
// 		Path:     "/",
// 		HttpOnly: false, // Allow JavaScript to read for header inclusion
// 		Secure:   r.TLS != nil,
// 		SameSite: http.SameSiteStrictMode,
// 		MaxAge:   3600, // 1 hour
// 	})

// 	return token
// }

// // For template rendering with CSRF
// func (s *Server) RenderTemplateWithCSRF(w http.ResponseWriter, r *http.Request, tmpl *template.Template, data interface{}) error {
// 	// Include CSRF token if needed
// 	if dataMap, ok := data.(map[string]interface{}); ok {
// 		if csrfToken := s.IncludeCSRFToken(w, r); csrfToken != "" {
// 			dataMap["CSRFToken"] = csrfToken
// 		}
// 	}

// 	return tmpl.Execute(w, data)
// }

// Generate a secure CSRF token
