// Package oidc verifies OpenID Connect ID tokens using the issuer's
// JWKS. Discovery is automatic (`<issuer>/.well-known/openid-configuration`),
// keys are cached and refreshed on `kid` miss, and the verifier handles
// RS256/RS384/RS512 + ES256/ES384/ES512.
//
// Designed as a per-route auth: configure once, drop into a Route's
// `auth: ["okta"]` list (after registering it under the global `auth:`
// block of server.yaml). Plays nicely with the existing JWT auth — they
// share the same JWT library and claims shape.
package oidc

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

// Config drives one Verifier instance.
type Config struct {
	Issuer       string        // e.g. "https://accounts.google.com"
	ClientID     string        // expected `aud` claim
	RefreshEvery time.Duration // JWKS poll interval; default 1h
	HTTPClient   *http.Client  // optional override (tests)
	NowFn        func() time.Time
}

// Claims is what callers get on a successful verify. We expose the
// common OIDC fields by name plus a generic Map for everything else
// (custom claims, role mappings, etc.).
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Audience      []string
	Issuer        string
	IssuedAt      time.Time
	ExpiresAt     time.Time
	Map           map[string]any
}

// Verifier validates ID tokens against the configured issuer.
type Verifier struct {
	cfg     Config
	mu      sync.RWMutex
	jwksURL string
	keys    map[string]any // kid → *rsa.PublicKey or *ecdsa.PublicKey
	expires time.Time
}

// New constructs a Verifier and immediately fetches the discovery
// document so misconfiguration fails at boot, not request time.
func New(ctx context.Context, cfg Config) (*Verifier, error) {
	if cfg.Issuer == "" {
		return nil, fmt.Errorf("oidc: empty issuer")
	}
	if cfg.RefreshEvery == 0 {
		cfg.RefreshEvery = time.Hour
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: 10 * time.Second}
	}
	if cfg.NowFn == nil {
		cfg.NowFn = time.Now
	}
	v := &Verifier{cfg: cfg, keys: map[string]any{}}
	if err := v.discover(ctx); err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	if err := v.refreshJWKS(ctx); err != nil {
		return nil, fmt.Errorf("oidc jwks: %w", err)
	}
	return v, nil
}

// Verify parses + validates an ID token string. Returns Claims on
// success, an error otherwise. Re-fetches JWKS once on `kid` miss to
// handle key rotation transparently.
func (v *Verifier) Verify(ctx context.Context, token string) (*Claims, error) {
	parsed, err := jwt.Parse(token, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if key, ok := v.lookupKey(kid); ok {
			return key, nil
		}
		// Maybe the IdP rotated keys — refresh once.
		if err := v.refreshJWKS(ctx); err != nil {
			return nil, err
		}
		if key, ok := v.lookupKey(kid); ok {
			return key, nil
		}
		return nil, fmt.Errorf("unknown kid %q", kid)
	}, jwt.WithIssuer(v.cfg.Issuer), jwt.WithExpirationRequired())
	if err != nil {
		return nil, err
	}
	if !parsed.Valid {
		return nil, errors.New("invalid token")
	}
	mc, ok := parsed.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("unexpected claims type")
	}

	if v.cfg.ClientID != "" {
		if !audienceMatches(mc["aud"], v.cfg.ClientID) {
			return nil, fmt.Errorf("audience mismatch")
		}
	}

	c := &Claims{Map: mc}
	c.Subject, _ = mc["sub"].(string)
	c.Email, _ = mc["email"].(string)
	c.EmailVerified, _ = mc["email_verified"].(bool)
	c.Name, _ = mc["name"].(string)
	c.Issuer, _ = mc["iss"].(string)
	switch a := mc["aud"].(type) {
	case string:
		c.Audience = []string{a}
	case []any:
		for _, x := range a {
			if s, ok := x.(string); ok {
				c.Audience = append(c.Audience, s)
			}
		}
	}
	if iat, ok := mc["iat"].(float64); ok {
		c.IssuedAt = time.Unix(int64(iat), 0)
	}
	if exp, ok := mc["exp"].(float64); ok {
		c.ExpiresAt = time.Unix(int64(exp), 0)
	}
	return c, nil
}

// Middleware extracts an Authorization: Bearer token, verifies it, and
// passes Claims via the request context to downstream handlers under
// the key returned by ContextKey.
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.Header.Get("Authorization")
		if !strings.HasPrefix(raw, "Bearer ") {
			http.Error(w, "missing bearer token", http.StatusUnauthorized)
			return
		}
		c, err := v.Verify(r.Context(), strings.TrimPrefix(raw, "Bearer "))
		if err != nil {
			http.Error(w, "invalid token: "+err.Error(), http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, c)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

type ctxKey struct{}

// FromContext returns the Claims stashed by Middleware, or nil.
func FromContext(ctx context.Context) *Claims {
	if c, ok := ctx.Value(ctxKey{}).(*Claims); ok {
		return c
	}
	return nil
}

// ── internals ─────────────────────────────────────────────────────────────

func (v *Verifier) lookupKey(kid string) (any, bool) {
	v.mu.RLock()
	defer v.mu.RUnlock()
	k, ok := v.keys[kid]
	return k, ok
}

func (v *Verifier) discover(ctx context.Context) error {
	u := strings.TrimRight(v.cfg.Issuer, "/") + "/.well-known/openid-configuration"
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := v.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("discovery: status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var doc struct {
		Issuer  string `json:"issuer"`
		JWKSURI string `json:"jwks_uri"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return err
	}
	if doc.JWKSURI == "" {
		return errors.New("discovery: missing jwks_uri")
	}
	if doc.Issuer != "" && doc.Issuer != v.cfg.Issuer {
		return fmt.Errorf("issuer mismatch: discovery says %q, config says %q", doc.Issuer, v.cfg.Issuer)
	}
	v.mu.Lock()
	v.jwksURL = doc.JWKSURI
	v.mu.Unlock()
	return nil
}

func (v *Verifier) refreshJWKS(ctx context.Context) error {
	v.mu.RLock()
	jwksURL := v.jwksURL
	v.mu.RUnlock()
	if jwksURL == "" {
		return errors.New("jwks_uri not yet discovered")
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, jwksURL, nil)
	resp, err := v.cfg.HTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("jwks: status %d", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	var jwks struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if err := json.Unmarshal(body, &jwks); err != nil {
		return err
	}
	parsed := map[string]any{}
	for _, raw := range jwks.Keys {
		key, kid, err := parseJWK(raw)
		if err != nil || kid == "" {
			continue
		}
		parsed[kid] = key
	}
	v.mu.Lock()
	v.keys = parsed
	v.expires = v.cfg.NowFn().Add(v.cfg.RefreshEvery)
	v.mu.Unlock()
	if len(parsed) == 0 {
		return errors.New("jwks: no usable keys")
	}
	return nil
}

func parseJWK(raw json.RawMessage) (any, string, error) {
	var k struct {
		Kty string `json:"kty"`
		Kid string `json:"kid"`
		Alg string `json:"alg"`
		Use string `json:"use"`
		N   string `json:"n"`
		E   string `json:"e"`
		Crv string `json:"crv"`
		X   string `json:"x"`
		Y   string `json:"y"`
	}
	if err := json.Unmarshal(raw, &k); err != nil {
		return nil, "", err
	}
	switch k.Kty {
	case "RSA":
		nBytes, err := base64URLDecode(k.N)
		if err != nil {
			return nil, "", err
		}
		eBytes, err := base64URLDecode(k.E)
		if err != nil {
			return nil, "", err
		}
		e := 0
		for _, b := range eBytes {
			e = e<<8 | int(b)
		}
		return &rsa.PublicKey{N: new(big.Int).SetBytes(nBytes), E: e}, k.Kid, nil
	case "EC":
		var curve elliptic.Curve
		switch k.Crv {
		case "P-256":
			curve = elliptic.P256()
		case "P-384":
			curve = elliptic.P384()
		case "P-521":
			curve = elliptic.P521()
		default:
			return nil, "", fmt.Errorf("unsupported curve %q", k.Crv)
		}
		xb, err := base64URLDecode(k.X)
		if err != nil {
			return nil, "", err
		}
		yb, err := base64URLDecode(k.Y)
		if err != nil {
			return nil, "", err
		}
		return &ecdsa.PublicKey{Curve: curve, X: new(big.Int).SetBytes(xb), Y: new(big.Int).SetBytes(yb)}, k.Kid, nil
	default:
		return nil, "", fmt.Errorf("unsupported kty %q", k.Kty)
	}
}

func base64URLDecode(s string) ([]byte, error) {
	// JWKS values are base64url without padding.
	if pad := len(s) % 4; pad > 0 {
		s += strings.Repeat("=", 4-pad)
	}
	return base64.URLEncoding.DecodeString(s)
}

func audienceMatches(aud any, want string) bool {
	switch v := aud.(type) {
	case string:
		return v == want
	case []any:
		for _, x := range v {
			if s, ok := x.(string); ok && s == want {
				return true
			}
		}
	}
	return false
}

// IssuerHost is a small helper for test setups (returns the host part
// of an https://... issuer URL).
func IssuerHost(issuer string) string {
	u, err := url.Parse(issuer)
	if err != nil {
		return issuer
	}
	return u.Host
}
