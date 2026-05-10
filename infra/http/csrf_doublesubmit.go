package http

import (
	"crypto/hmac"
	"fmt"
	"net/http"
	"strings"
)

// ValidateDoubleSubmit performs a *real* CSRF check using the
// double-submit cookie pattern: the request must carry both the
// `csrf_token` cookie AND an `X-CSRF-Token` header (or `_csrf` form
// field) with an identical value. The original CSRFStore.Validate
// only checked cookie presence, which is bypassed by any cross-site
// attack that automatically includes cookies. This is the SPA-safe
// alternative.
//
// Safe methods (GET/HEAD/OPTIONS) bypass the check.
//
// Tokens still need to exist in the store (so we can revoke /
// rotate). Pass cleanup=true to delete the token on success
// (one-time-use semantics).
func (s *CSRFStore) ValidateDoubleSubmit(r *http.Request, cleanup bool) error {
	if r.Method == http.MethodGet || r.Method == http.MethodHead || r.Method == http.MethodOptions {
		return nil
	}
	cookie, err := r.Cookie("csrf_token")
	if err != nil || cookie.Value == "" {
		return fmt.Errorf("CSRF cookie missing")
	}
	header := r.Header.Get("X-CSRF-Token")
	if header == "" {
		// Fall back to a form field for non-JS clients.
		_ = r.ParseForm()
		header = r.PostForm.Get("_csrf")
	}
	if header == "" {
		return fmt.Errorf("CSRF header/field missing")
	}
	if !hmac.Equal([]byte(cookie.Value), []byte(header)) {
		return fmt.Errorf("CSRF token mismatch")
	}

	s.mutex.RLock()
	_, found := s.tokens[cookie.Value]
	s.mutex.RUnlock()
	if !found {
		return fmt.Errorf("CSRF token not in store (expired?)")
	}
	if cleanup {
		s.mutex.Lock()
		delete(s.tokens, cookie.Value)
		s.mutex.Unlock()
	}
	return nil
}

// MiddlewareDoubleSubmit wraps next with double-submit CSRF
// validation. Unsafe-method requests without a matching cookie+header
// pair get 403. cleanup=true makes the token one-time-use; cleanup=false
// keeps the token reusable until its TTL expires (matches the
// developer-friendly default for SPAs that issue many requests per
// page).
func (s *CSRFStore) MiddlewareDoubleSubmit(cleanup bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := s.ValidateDoubleSubmit(r, cleanup); err != nil {
				http.Error(w, "forbidden: "+err.Error(), http.StatusForbidden)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// SetCSRFCookieJSAccessible sets the CSRF cookie *without* HttpOnly so
// SPA JS can read its value and echo it back as X-CSRF-Token. The
// existing GenerateToken in csrf.go sets HttpOnly which prevents
// double-submit from working at all — call this from the same login /
// page-load handlers when you've opted into the double-submit pattern.
//
// This is intentionally a no-Op-besides-issuance helper: callers also
// need to call s.GenerateToken to populate the in-memory store. The
// usual pattern:
//
//	tok := store.GenerateToken(w, r)         // store + HttpOnly cookie
//	SetCSRFCookieJSAccessible(w, r, tok)     // mirror as JS-readable cookie
func SetCSRFCookieJSAccessible(w http.ResponseWriter, r *http.Request, token string) {
	http.SetCookie(w, &http.Cookie{
		Name:     "csrf_token_js",
		Value:    token,
		Path:     "/",
		HttpOnly: false, // intentionally readable by JS
		Secure:   r.TLS != nil,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   3600,
	})
}

// HeaderTokenFromCookie is a tiny helper for tests/curl scripts: lifts
// the CSRF token out of the cookie jar so callers can synthesize the
// matching header.
func HeaderTokenFromCookie(r *http.Request) string {
	c, err := r.Cookie("csrf_token")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(c.Value)
}
