package oidc

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"math/big"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

// fakeOP boots a tiny OpenID Provider exposing /.well-known/openid-configuration
// and a JWKS endpoint, signs ID tokens with a generated RSA key, and lets
// tests rotate the key on demand.
type fakeOP struct {
	t        *testing.T
	srv      *httptest.Server
	priv     *rsa.PrivateKey
	kid      string
	clientID string
}

func newFakeOP(t *testing.T) *fakeOP {
	t.Helper()
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	op := &fakeOP{t: t, priv: priv, kid: "k1", clientID: "test-client"}
	mux := http.NewServeMux()
	op.srv = httptest.NewServer(mux)

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":   op.srv.URL,
			"jwks_uri": op.srv.URL + "/jwks",
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(op.jwks())
	})
	return op
}

func (op *fakeOP) jwks() map[string]any {
	pub := op.priv.Public().(*rsa.PublicKey)
	n := base64.RawURLEncoding.EncodeToString(pub.N.Bytes())
	eb := big.NewInt(int64(pub.E)).Bytes()
	return map[string]any{
		"keys": []map[string]any{{
			"kty": "RSA", "alg": "RS256", "use": "sig", "kid": op.kid,
			"n": n, "e": base64.RawURLEncoding.EncodeToString(eb),
		}},
	}
}

func (op *fakeOP) rotate() {
	priv, _ := rsa.GenerateKey(rand.Reader, 2048)
	op.priv = priv
	op.kid = "k" + fmt.Sprint(time.Now().UnixNano())
}

func (op *fakeOP) signToken(t *testing.T, claims jwt.MapClaims) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodRS256, claims)
	tok.Header["kid"] = op.kid
	s, err := tok.SignedString(op.priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func (op *fakeOP) issuerURL() string { return op.srv.URL }

func TestVerifyValidToken(t *testing.T) {
	op := newFakeOP(t)
	defer op.srv.Close()

	v, err := New(context.Background(), Config{
		Issuer: op.issuerURL(), ClientID: op.clientID,
	})
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().Unix()
	tok := op.signToken(t, jwt.MapClaims{
		"iss": op.issuerURL(), "aud": op.clientID,
		"sub": "user-1", "email": "u@x.io", "email_verified": true,
		"iat": now, "exp": now + 300,
	})
	c, err := v.Verify(context.Background(), tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "user-1" || c.Email != "u@x.io" || !c.EmailVerified {
		t.Errorf("claims = %+v", c)
	}
}

func TestVerifyAudienceMismatch(t *testing.T) {
	op := newFakeOP(t)
	defer op.srv.Close()
	v, _ := New(context.Background(), Config{Issuer: op.issuerURL(), ClientID: "expected"})
	tok := op.signToken(t, jwt.MapClaims{
		"iss": op.issuerURL(), "aud": "wrong",
		"sub": "u", "exp": time.Now().Add(time.Minute).Unix(),
	})
	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Error("expected audience mismatch")
	}
}

func TestVerifyExpiredToken(t *testing.T) {
	op := newFakeOP(t)
	defer op.srv.Close()
	v, _ := New(context.Background(), Config{Issuer: op.issuerURL(), ClientID: op.clientID})
	tok := op.signToken(t, jwt.MapClaims{
		"iss": op.issuerURL(), "aud": op.clientID,
		"sub": "u", "exp": time.Now().Add(-time.Minute).Unix(),
	})
	if _, err := v.Verify(context.Background(), tok); err == nil {
		t.Error("expected expired-token error")
	}
}

func TestVerifyKeyRotationTransparent(t *testing.T) {
	op := newFakeOP(t)
	defer op.srv.Close()
	v, err := New(context.Background(), Config{Issuer: op.issuerURL(), ClientID: op.clientID})
	if err != nil {
		t.Fatal(err)
	}

	// Server rotates its key. Existing JWKS cache no longer covers it.
	op.rotate()
	tok := op.signToken(t, jwt.MapClaims{
		"iss": op.issuerURL(), "aud": op.clientID,
		"sub": "u", "exp": time.Now().Add(time.Minute).Unix(),
	})
	if _, err := v.Verify(context.Background(), tok); err != nil {
		t.Errorf("rotation should be handled transparently: %v", err)
	}
}

func TestMiddleware401WithoutBearer(t *testing.T) {
	op := newFakeOP(t)
	defer op.srv.Close()
	v, _ := New(context.Background(), Config{Issuer: op.issuerURL(), ClientID: op.clientID})
	h := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := FromContext(r.Context())
		if c == nil {
			t.Fatal("FromContext should be non-nil")
		}
		fmt.Fprint(w, "ok")
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status without Bearer = %d", w.Code)
	}
}

func TestMiddleware200WithValidToken(t *testing.T) {
	op := newFakeOP(t)
	defer op.srv.Close()
	v, _ := New(context.Background(), Config{Issuer: op.issuerURL(), ClientID: op.clientID})
	h := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c := FromContext(r.Context())
		fmt.Fprint(w, c.Subject)
	}))

	tok := op.signToken(t, jwt.MapClaims{
		"iss": op.issuerURL(), "aud": op.clientID,
		"sub": "alice", "exp": time.Now().Add(time.Minute).Unix(),
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || !strings.Contains(w.Body.String(), "alice") {
		t.Errorf("status=%d body=%q", w.Code, w.Body.String())
	}
}

func TestNewRejectsBadIssuer(t *testing.T) {
	if _, err := New(context.Background(), Config{Issuer: ""}); err == nil {
		t.Error("expected empty-issuer error")
	}
	if _, err := New(context.Background(), Config{
		Issuer: "http://127.0.0.1:1", // unreachable
	}); err == nil {
		t.Error("expected discovery error")
	}
}
