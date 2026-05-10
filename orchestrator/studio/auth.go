package studio

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// loadOrCreateToken returns the studio bearer token, generating a new
// 32-byte random one (hex-encoded) if the file does not exist.
// Token file is mode 0600.
func loadOrCreateToken(dataDir string) (string, error) {
	if err := os.MkdirAll(dataDir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dataDir, "studio.token")
	bytes, err := os.ReadFile(path)
	if err == nil && len(bytes) > 0 {
		return strings.TrimSpace(string(bytes)), nil
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(raw)
	if err := os.WriteFile(path, []byte(tok), 0o600); err != nil {
		return "", err
	}
	return tok, nil
}

// authMiddleware checks Authorization: Bearer <token> OR studio_token
// cookie. Rejects everything else with 401.
func authMiddleware(token string, next http.Handler) http.Handler {
	expected := []byte(token)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checkToken(r, expected) {
			next.ServeHTTP(w, r)
			return
		}
		w.Header().Set("WWW-Authenticate", `Bearer realm="studio"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	})
}

func checkToken(r *http.Request, expected []byte) bool {
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		got := strings.TrimSpace(strings.TrimPrefix(h, "Bearer "))
		if subtle.ConstantTimeCompare([]byte(got), expected) == 1 {
			return true
		}
	}
	if c, err := r.Cookie("studio_token"); err == nil && c.Value != "" {
		if subtle.ConstantTimeCompare([]byte(c.Value), expected) == 1 {
			return true
		}
	}
	return false
}
