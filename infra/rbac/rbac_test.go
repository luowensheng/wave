package rbac

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestEmptyPolicyAllowsAll(t *testing.T) {
	if err := (Policy{}).Check(nil); err != nil {
		t.Errorf("empty policy rejected: %v", err)
	}
}

func TestRoleListMatchesAnyKey(t *testing.T) {
	p := Policy{Roles: []string{"admin"}}
	for _, claims := range []map[string]any{
		{"roles": []any{"admin", "billing"}},
		{"groups": []string{"admin"}},
		{"permissions": []any{"admin"}},
		{"scope": "openid email admin profile"},
	} {
		if err := p.Check(claims); err != nil {
			t.Errorf("expected match in %v: %v", claims, err)
		}
	}
}

func TestRoleMissing(t *testing.T) {
	p := Policy{Roles: []string{"admin", "billing"}}
	err := p.Check(map[string]any{"roles": []any{"admin"}})
	if err == nil {
		t.Error("expected missing-role error")
	}
}

func TestClaimExactMatch(t *testing.T) {
	p := Policy{Claims: map[string]string{"plan": "enterprise", "region": "us-east"}}
	if err := p.Check(map[string]any{"plan": "enterprise", "region": "us-east", "x": 1}); err != nil {
		t.Errorf("unexpected reject: %v", err)
	}
	if err := p.Check(map[string]any{"plan": "enterprise"}); err == nil {
		t.Error("missing region should reject")
	}
	if err := p.Check(map[string]any{"plan": "starter", "region": "us-east"}); err == nil {
		t.Error("wrong plan should reject")
	}
}

func TestMiddleware403(t *testing.T) {
	mw := Middleware(Policy{Roles: []string{"admin"}})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d", w.Code)
	}
}

func TestMiddleware200WithClaims(t *testing.T) {
	mw := Middleware(Policy{Roles: []string{"admin"}})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	ctx := WithClaims(context.Background(), map[string]any{"roles": []any{"admin"}})
	r := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}
}

func TestEmptyPolicyMiddlewarePassThrough(t *testing.T) {
	mw := Middleware(Policy{})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != http.StatusTeapot {
		t.Errorf("empty policy should be transparent, status=%d", w.Code)
	}
}
