package oauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

// apple is the "Sign in with Apple" provider. Two real quirks:
//
//   1. The OAuth client_secret is *not* a static string; it's an
//      ES256-signed JWT that we generate per-token-exchange using a
//      .p8 private key Apple gave you in their dev portal. The JWT
//      identifies your team + key + service ID and is good for up to
//      6 months.
//
//   2. There is NO userinfo endpoint. The user's identity arrives
//      in the `id_token` field of the token response — a JWT signed
//      with Apple's JWKS. We decode it without verification (this
//      package's job is the OAuth dance; signature verification of the
//      id_token is a future PR, alongside infra/oidc-style JWKS
//      fetching of https://appleid.apple.com/auth/keys).
type apple struct {
	cfg         Config
	client      *http.Client
	authorizeURL string
	tokenURL     string
	privateKey  *ecdsa.PrivateKey
}

func newApple(c Config) (Provider, error) {
	if c.ClientID == "" {
		return nil, fmt.Errorf("oauth apple: client_id (your Service ID) required")
	}
	if c.AppleTeamID == "" || c.AppleKeyID == "" {
		return nil, fmt.Errorf("oauth apple: apple_team_id and apple_key_id required")
	}
	if c.ApplePrivateKeyPath == "" && c.ApplePrivateKeyPEM == "" {
		return nil, fmt.Errorf("oauth apple: apple_private_key_path or apple_private_key_pem required")
	}
	if len(c.Scopes) == 0 {
		c.Scopes = []string{"name", "email"}
	}

	pemBytes := []byte(c.ApplePrivateKeyPEM)
	if len(pemBytes) == 0 {
		b, err := os.ReadFile(c.ApplePrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("read apple p8 key: %w", err)
		}
		pemBytes = b
	}
	priv, err := parseECPrivateKey(pemBytes)
	if err != nil {
		return nil, fmt.Errorf("parse apple p8 key: %w", err)
	}
	return &apple{
		cfg:          c,
		client:       &http.Client{Timeout: 10 * time.Second},
		authorizeURL: defaultIfEmpty(c.AuthorizeURL, "https://appleid.apple.com/auth/authorize"),
		tokenURL:     defaultIfEmpty(c.TokenURL, "https://appleid.apple.com/auth/token"),
		privateKey:   priv,
	}, nil
}

func (a *apple) Name() string { return "apple" }

func (a *apple) AuthorizeURL(state, redirectURI string) string {
	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", a.cfg.ClientID)
	v.Set("redirect_uri", redirectURI)
	v.Set("state", state)
	v.Set("scope", strings.Join(a.cfg.Scopes, " "))
	v.Set("response_mode", "query") // simpler than form_post; supportable later
	return a.authorizeURL + "?" + v.Encode()
}

// Exchange mints the per-call client_secret JWT and POSTs it.
func (a *apple) Exchange(ctx context.Context, code, redirectURI string) (string, error) {
	secret, err := a.signClientSecret()
	if err != nil {
		return "", err
	}
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", a.cfg.ClientID)
	form.Set("client_secret", secret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("apple token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("apple token status %d: %s", resp.StatusCode, string(body))
	}
	// Apple returns access_token AND id_token. We pack both into a
	// pipe-separated string so GetUserInfo can pull out id_token (the
	// real source of identity for Apple).
	var tr struct {
		AccessToken string `json:"access_token"`
		IDToken     string `json:"id_token"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("decode apple token: %w", err)
	}
	if tr.IDToken == "" {
		return "", fmt.Errorf("apple token response missing id_token")
	}
	// Encode both pieces so GetUserInfo can recover them. The "access
	// token" returned to the orchestrator is opaque to it.
	return tr.AccessToken + "|" + tr.IDToken, nil
}

// GetUserInfo for Apple decodes the JWT we packed into the access
// token field. **Note**: this PR doesn't verify the JWT signature
// against Apple's JWKS — that's a follow-up using infra/oidc's JWKS
// machinery. For now we trust the id_token because we just received
// it from Apple over TLS in Exchange.
func (a *apple) GetUserInfo(ctx context.Context, packed string) (*Claims, error) {
	parts := strings.SplitN(packed, "|", 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("apple: malformed packed token")
	}
	idToken := parts[1]
	tok, _, err := new(jwt.Parser).ParseUnverified(idToken, jwt.MapClaims{})
	if err != nil {
		return nil, fmt.Errorf("parse id_token: %w", err)
	}
	mc, ok := tok.Claims.(jwt.MapClaims)
	if !ok {
		return nil, fmt.Errorf("unexpected id_token claims type")
	}
	c := claimsFromMap(map[string]any(mc), "apple")
	c.Subject, _ = mc["sub"].(string)
	c.Email, _ = mc["email"].(string)
	if v, ok := mc["email_verified"].(string); ok {
		c.EmailVerified = v == "true"
	} else if v, ok := mc["email_verified"].(bool); ok {
		c.EmailVerified = v
	}
	return c, nil
}

// signClientSecret mints the ES256 JWT Apple expects in place of a
// shared client secret.
func (a *apple) signClientSecret() (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{
		"iss": a.cfg.AppleTeamID,
		"iat": now.Unix(),
		"exp": now.Add(15 * time.Minute).Unix(), // short TTL is fine; we mint per request
		"aud": "https://appleid.apple.com",
		"sub": a.cfg.ClientID,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, claims)
	tok.Header["kid"] = a.cfg.AppleKeyID
	tok.Header["alg"] = "ES256"
	return tok.SignedString(a.privateKey)
}

// parseECPrivateKey accepts the .p8 format Apple distributes (PKCS#8
// PEM). Falls back to the older SEC1 EC PRIVATE KEY format if needed.
func parseECPrivateKey(pemBytes []byte) (*ecdsa.PrivateKey, error) {
	block, _ := pem.Decode(pemBytes)
	if block == nil {
		return nil, fmt.Errorf("no PEM data")
	}
	if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
		ec, ok := k.(*ecdsa.PrivateKey)
		if !ok {
			return nil, fmt.Errorf("PKCS#8 key is not ECDSA")
		}
		return ec, nil
	}
	return x509.ParseECPrivateKey(block.Bytes)
}
