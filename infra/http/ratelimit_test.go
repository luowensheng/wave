package http

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTokenBucketAllowsBurst(t *testing.T) {
	tb := NewTokenBucket(1, 3) // 1 rps, burst 3
	for i := 0; i < 3; i++ {
		if !tb.Allow("k") {
			t.Fatalf("burst rejected at i=%d", i)
		}
	}
	if tb.Allow("k") {
		t.Error("4th call should reject")
	}
}

func TestTokenBucketRefills(t *testing.T) {
	tb := NewTokenBucket(100, 1) // 100 rps means 1 token in 10ms
	if !tb.Allow("k") {
		t.Fatal("first call rejected")
	}
	if tb.Allow("k") {
		t.Fatal("second immediate call should reject")
	}
	time.Sleep(20 * time.Millisecond)
	if !tb.Allow("k") {
		t.Error("call after refill should succeed")
	}
}

func TestTokenBucketKeysAreIndependent(t *testing.T) {
	tb := NewTokenBucket(1, 1)
	if !tb.Allow("a") || !tb.Allow("b") {
		t.Fatal("independent keys should each allow once")
	}
	if tb.Allow("a") {
		t.Error("key a should now reject")
	}
}

func TestTokenBucketMiddleware(t *testing.T) {
	tb := NewTokenBucket(1, 1)
	mw := tb.Middleware(func(r *http.Request) string { return "k" })
	h := mw(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, httptest.NewRequest("GET", "/", nil))
	if w1.Code != 200 {
		t.Errorf("first = %d", w1.Code)
	}
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, httptest.NewRequest("GET", "/", nil))
	if w2.Code != http.StatusTooManyRequests {
		t.Errorf("second = %d", w2.Code)
	}
	if w2.Header().Get("Retry-After") == "" {
		t.Error("Retry-After missing")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("X-Real-IP", "1.2.3.4")
	if got := ClientIP(r); got != "1.2.3.4" {
		t.Errorf("got %q", got)
	}

	r2 := httptest.NewRequest("GET", "/", nil)
	r2.Header.Set("X-Forwarded-For", "9.9.9.9, 10.0.0.1")
	if got := ClientIP(r2); got != "9.9.9.9" {
		t.Errorf("xff: got %q", got)
	}
}
