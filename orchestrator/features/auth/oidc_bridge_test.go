package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

// minimal in-line fake OP — doesn't share code with infra/oidc tests
// so this stays a true integration test.
type fakeOP struct {
	srv  *httptest.Server
	priv *rsa.PrivateKey
	kid  string
}

func newFakeOP(t *testing.T) *fakeOP {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	op := &fakeOP{priv: priv, kid: "kbridge"}
	mux := http.NewServeMux()
	op.srv = httptest.NewServer(mux)
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer": op.srv.URL, "jwks_uri": op.srv.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		pub := op.priv.Public().(*rsa.PublicKey)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA", "alg": "RS256", "use": "sig", "kid": op.kid,
				"n": base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
				"e": base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
			}},
		})
	})
	return op
}

func (op *fakeOP) sign(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = op.kid
	s, err := tok.SignedString(op.priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAuthManagerOIDCEndToEnd(t *testing.T) {
	op := newFakeOP(t)
	defer op.srv.Close()

	// Boot the real auth manager with a single oidc config.
	t.Setenv("SECRET_KEY", "test-jwt-secret-not-used-for-oidc")
	cfg := map[string]*AuthConfig{
		"corp": {
			Type:          "oidc",
			TokenLocation: "header",
			HeaderName:    "Authorization",
			HeaderScheme:  "Bearer",
			Issuer:        op.srv.URL,
			ClientID:      "test-app",
		},
	}
	if err := InitAuthManager(cfg); err != nil {
		t.Fatal(err)
	}

	// Real RequireAuth wrapping a "you got in" handler.
	h := RequireAuth(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := r.Context().Value(UserContextKey).(*PublicUser)
		_, _ = w.Write([]byte("hello, " + u.Username))
	}), "corp")

	// Valid token → 200 + email in body.
	tok := op.sign(t, jwt.MapClaims{
		"iss": op.srv.URL, "aud": "test-app",
		"sub": "user-1", "email": "alice@example.com",
		"exp": time.Now().Add(time.Minute).Unix(),
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d, body=%q", w.Code, w.Body.String())
	}
	if got := w.Body.String(); got != "hello, alice@example.com" {
		t.Errorf("body = %q", got)
	}

	// Wrong audience → 401.
	tokBad := op.sign(t, jwt.MapClaims{
		"iss": op.srv.URL, "aud": "other-app",
		"sub": "u", "exp": time.Now().Add(time.Minute).Unix(),
	})
	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Set("Authorization", "Bearer "+tokBad)
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)
	if w2.Code != http.StatusUnauthorized {
		t.Errorf("bad-aud status = %d", w2.Code)
	}

	// Missing token → 401.
	w3 := httptest.NewRecorder()
	h.ServeHTTP(w3, httptest.NewRequest("GET", "/", nil))
	if w3.Code != http.StatusUnauthorized {
		t.Errorf("no-token status = %d", w3.Code)
	}
}

func TestSetupOIDCRequiresIssuerAndClientID(t *testing.T) {
	err := setupOIDC(map[string]*AuthConfig{
		"x": {Type: "oidc"},
	})
	if err == nil {
		t.Error("expected error for missing issuer/client_id")
	}
}
