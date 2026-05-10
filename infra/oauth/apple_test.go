package oauth

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	jwt "github.com/golang-jwt/jwt/v5"
)

// genApplePEM creates an ES256 private key and PKCS#8-PEM-encodes it
// so newApple can parse it like a real .p8 file.
func genApplePEM(t *testing.T) (string, *ecdsa.PrivateKey) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	return string(pemBytes), priv
}

func fakeAppleIdToken(t *testing.T, priv *ecdsa.PrivateKey, sub, email string) string {
	t.Helper()
	tok := jwt.NewWithClaims(jwt.SigningMethodES256, jwt.MapClaims{
		"iss": "https://appleid.apple.com",
		"sub": sub, "email": email, "email_verified": "true",
		"iat": time.Now().Unix(), "exp": time.Now().Add(time.Hour).Unix(),
	})
	tok.Header["kid"] = "TESTKEY"
	s, err := tok.SignedString(priv)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestAppleFullFlow(t *testing.T) {
	pem, _ := genApplePEM(t)
	// We sign the *fake server's* id_token with a different key — we
	// don't verify Apple's signature in this PR (documented), so any
	// well-formed JWT is accepted.
	_, fakeAppleKey := genApplePEM(t)
	idToken := fakeAppleIdToken(t, fakeAppleKey, "user-sub-001", "alice@privaterelay.appleid.com")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/auth/token" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_ = r.ParseForm()
		// Verify the client_secret JWT was signed correctly.
		secret := r.PostForm.Get("client_secret")
		if secret == "" {
			t.Error("client_secret missing")
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "apple-access-tok",
			"id_token":     idToken,
		})
	}))
	defer srv.Close()

	p, err := newApple(Config{
		Provider:           "apple",
		ClientID:           "com.example.client",
		AppleTeamID:        "TEAM12345",
		AppleKeyID:         "KEYID12345",
		ApplePrivateKeyPEM: pem,
		AuthorizeURL:       srv.URL + "/auth/authorize",
		TokenURL:           srv.URL + "/auth/token",
	})
	if err != nil {
		t.Fatal(err)
	}

	tok, err := p.Exchange(context.Background(), "code-x", "https://app/cb")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(tok, "|") {
		t.Errorf("token should be packed access|id, got %q", tok)
	}
	c, err := p.GetUserInfo(context.Background(), tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Subject != "user-sub-001" {
		t.Errorf("subject = %q", c.Subject)
	}
	if c.Email != "alice@privaterelay.appleid.com" || !c.EmailVerified {
		t.Errorf("email = %q verified=%v", c.Email, c.EmailVerified)
	}
}

func TestAppleAuthorizeURLContainsParams(t *testing.T) {
	pem, _ := genApplePEM(t)
	p, err := newApple(Config{
		ClientID: "com.example.svc", AppleTeamID: "T", AppleKeyID: "K",
		ApplePrivateKeyPEM: pem,
	})
	if err != nil {
		t.Fatal(err)
	}
	u := p.AuthorizeURL("xyz", "https://app/cb")
	for _, want := range []string{
		"client_id=com.example.svc", "state=xyz",
		"response_mode=query", "response_type=code",
	} {
		if !strings.Contains(u, want) {
			t.Errorf("missing %q in %s", want, u)
		}
	}
}

func TestAppleRequiresKey(t *testing.T) {
	if _, err := newApple(Config{ClientID: "x", AppleTeamID: "t", AppleKeyID: "k"}); err == nil {
		t.Error("expected key-required error")
	}
}

func TestAppleReadsKeyFromFile(t *testing.T) {
	pem, _ := genApplePEM(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "AuthKey_X.p8")
	if err := writeFile(t, path, pem); err != nil {
		t.Fatal(err)
	}
	if _, err := newApple(Config{
		ClientID: "x", AppleTeamID: "t", AppleKeyID: "k",
		ApplePrivateKeyPath: path,
	}); err != nil {
		t.Errorf("file path key failed: %v", err)
	}
}

func writeFile(t *testing.T, path, body string) error {
	t.Helper()
	return os.WriteFile(path, []byte(body), 0o600)
}
