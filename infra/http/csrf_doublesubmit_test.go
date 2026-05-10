package http

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newStoreWithToken(t *testing.T) (*CSRFStore, string) {
	t.Helper()
	s := NewCSRFStore()
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	tok := s.GenerateToken(w, r)
	if tok == "" {
		t.Fatal("token empty")
	}
	return s, tok
}

func TestDoubleSubmitOK(t *testing.T) {
	s, tok := newStoreWithToken(t)
	r := httptest.NewRequest("POST", "/x", nil)
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: tok})
	r.Header.Set("X-CSRF-Token", tok)

	if err := s.ValidateDoubleSubmit(r, false); err != nil {
		t.Errorf("expected pass, got %v", err)
	}
}

func TestDoubleSubmitMissingHeader(t *testing.T) {
	s, tok := newStoreWithToken(t)
	r := httptest.NewRequest("POST", "/x", nil)
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: tok})
	if err := s.ValidateDoubleSubmit(r, false); err == nil {
		t.Error("expected reject")
	}
}

func TestDoubleSubmitMismatchedToken(t *testing.T) {
	s, tok := newStoreWithToken(t)
	r := httptest.NewRequest("POST", "/x", nil)
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: tok})
	r.Header.Set("X-CSRF-Token", "different")
	if err := s.ValidateDoubleSubmit(r, false); err == nil {
		t.Error("expected mismatch error")
	}
}

func TestDoubleSubmitFormFallback(t *testing.T) {
	s, tok := newStoreWithToken(t)
	r := httptest.NewRequest("POST", "/x",
		strings.NewReader("_csrf="+tok+"&other=v"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: tok})
	if err := s.ValidateDoubleSubmit(r, false); err != nil {
		t.Errorf("form field fallback failed: %v", err)
	}
}

func TestDoubleSubmitSafeMethodBypass(t *testing.T) {
	s, _ := newStoreWithToken(t)
	for _, m := range []string{"GET", "HEAD", "OPTIONS"} {
		r := httptest.NewRequest(m, "/x", nil)
		if err := s.ValidateDoubleSubmit(r, false); err != nil {
			t.Errorf("%s should bypass, got %v", m, err)
		}
	}
}

func TestDoubleSubmitOneTimeUse(t *testing.T) {
	s, tok := newStoreWithToken(t)
	r := httptest.NewRequest("POST", "/x", nil)
	r.AddCookie(&http.Cookie{Name: "csrf_token", Value: tok})
	r.Header.Set("X-CSRF-Token", tok)
	if err := s.ValidateDoubleSubmit(r, true); err != nil {
		t.Fatal(err)
	}
	// Second call should fail because cleanup=true removed the token.
	r2 := httptest.NewRequest("POST", "/x", nil)
	r2.AddCookie(&http.Cookie{Name: "csrf_token", Value: tok})
	r2.Header.Set("X-CSRF-Token", tok)
	if err := s.ValidateDoubleSubmit(r2, true); err == nil {
		t.Error("expected token-not-in-store on second use")
	}
}

func TestMiddlewareDoubleSubmit403(t *testing.T) {
	s := NewCSRFStore()
	mw := s.MiddlewareDoubleSubmit(false)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not reach handler")
	}))
	r := httptest.NewRequest("POST", "/x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusForbidden {
		t.Errorf("status = %d", w.Code)
	}
}
