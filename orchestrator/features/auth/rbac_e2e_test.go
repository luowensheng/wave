package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/luowensheng/wave/infra/rbac"

	jwt "github.com/golang-jwt/jwt/v5"
)

func TestOIDCRBACRoleAllowed(t *testing.T) {
	op := newFakeOP(t)
	defer op.srv.Close()
	t.Setenv("SECRET_KEY", "x")
	if err := InitAuthManager(map[string]*AuthConfig{
		"corp": {
			Type: "oidc", TokenLocation: "header", HeaderName: "Authorization",
			HeaderScheme: "Bearer", Issuer: op.srv.URL, ClientID: "app",
		},
	}); err != nil {
		t.Fatal(err)
	}

	policy := rbac.Policy{Roles: []string{"admin"}}
	h := RequireAuth(rbac.Middleware(policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})), "corp")

	tok := op.sign(t, jwt.MapClaims{
		"iss": op.srv.URL, "aud": "app", "sub": "u",
		"exp": time.Now().Add(time.Minute).Unix(),
		"roles": []string{"admin", "user"},
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
	}
}

func TestOIDCRBACRoleDenied(t *testing.T) {
	op := newFakeOP(t)
	defer op.srv.Close()
	t.Setenv("SECRET_KEY", "x")
	if err := InitAuthManager(map[string]*AuthConfig{
		"corp": {
			Type: "oidc", TokenLocation: "header", HeaderName: "Authorization",
			HeaderScheme: "Bearer", Issuer: op.srv.URL, ClientID: "app",
		},
	}); err != nil {
		t.Fatal(err)
	}

	policy := rbac.Policy{Roles: []string{"admin"}}
	h := RequireAuth(rbac.Middleware(policy)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	})), "corp")

	tok := op.sign(t, jwt.MapClaims{
		"iss": op.srv.URL, "aud": "app", "sub": "u",
		"exp":   time.Now().Add(time.Minute).Unix(),
		"roles": []string{"user"}, // missing admin
	})
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d", w.Code)
	}
}
