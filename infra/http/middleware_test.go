package http

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRequestIDMiddlewareGenerates(t *testing.T) {
	var got string
	h := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = RequestIDFrom(r.Context())
	}))
	r := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if got == "" {
		t.Fatal("no request id on context")
	}
	if w.Header().Get("X-Request-Id") != got {
		t.Errorf("response header mismatch: %q vs %q", w.Header().Get("X-Request-Id"), got)
	}
}

func TestRequestIDMiddlewareEchoes(t *testing.T) {
	h := RequestIDMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Request-Id", "abc123")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Header().Get("X-Request-Id") != "abc123" {
		t.Errorf("got %q", w.Header().Get("X-Request-Id"))
	}
}

func TestSecurityHeadersDefaults(t *testing.T) {
	mw := SecurityHeadersMiddleware(SecurityHeadersConfig{})
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Header().Get("X-Frame-Options") != "DENY" {
		t.Error("X-Frame-Options default missing")
	}
	if w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Error("nosniff missing")
	}
	if w.Header().Get("Referrer-Policy") == "" {
		t.Error("Referrer-Policy missing")
	}
}

func TestMaxBodyMiddlewareRejects(t *testing.T) {
	mw := MaxBodyMiddleware(8)
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	r := httptest.NewRequest("POST", "/", strings.NewReader("12345678901234"))
	r.ContentLength = 14
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("got %d", w.Code)
	}
}

func TestRequestIDFromMissing(t *testing.T) {
	if id := RequestIDFrom(context.Background()); id != "" {
		t.Errorf("expected empty id, got %q", id)
	}
}
