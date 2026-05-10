package forwardauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

func TestForwardAuthAllowsOn2xx(t *testing.T) {
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-User", "alice")
		w.WriteHeader(204)
	}))
	defer authSrv.Close()

	v, _ := New(Config{URL: authSrv.URL, ResponseHeaders: []string{"X-User"}})
	var got string
	h := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("X-User")
		w.WriteHeader(200)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 200 {
		t.Errorf("status = %d", w.Code)
	}
	if got != "alice" {
		t.Errorf("X-User on inner request = %q", got)
	}
}

func TestForwardAuthRejectsAndMirrorsBody(t *testing.T) {
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "/login")
		w.WriteHeader(302)
		_, _ = w.Write([]byte("redirecting..."))
	}))
	defer authSrv.Close()

	v, _ := New(Config{URL: authSrv.URL})
	called := atomic.Int64{}
	h := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called.Add(1)
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/protected", nil))
	if called.Load() != 0 {
		t.Errorf("inner handler should not run")
	}
	if w.Code != 302 || w.Header().Get("Location") != "/login" {
		t.Errorf("status=%d location=%q", w.Code, w.Header().Get("Location"))
	}
	if !strings.Contains(w.Body.String(), "redirecting") {
		t.Errorf("body not mirrored: %q", w.Body.String())
	}
}

func TestForwardAuthForwardsHeaders(t *testing.T) {
	var seen string
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = r.Header.Get("Cookie")
		w.WriteHeader(204)
	}))
	defer authSrv.Close()

	v, _ := New(Config{URL: authSrv.URL})
	h := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Cookie", "session=abc")
	h.ServeHTTP(httptest.NewRecorder(), r)
	if seen != "session=abc" {
		t.Errorf("cookie not forwarded: %q", seen)
	}
}

func TestForwardAuthForwardsXFFOnTrust(t *testing.T) {
	var method string
	authSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method = r.Header.Get("X-Forwarded-Method")
		w.WriteHeader(204)
	}))
	defer authSrv.Close()

	v, _ := New(Config{URL: authSrv.URL, TrustForwardedFor: true})
	h := v.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	r := httptest.NewRequest("DELETE", "/things/42", nil)
	h.ServeHTTP(httptest.NewRecorder(), r)
	if method != "DELETE" {
		t.Errorf("X-Forwarded-Method = %q", method)
	}
}

func TestForwardAuthRejectsEmptyURL(t *testing.T) {
	if _, err := New(Config{}); err == nil {
		t.Error("expected error")
	}
}
