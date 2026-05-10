package http

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheHitsAndMisses(t *testing.T) {
	var hits atomic.Int64
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprintf(w, "hi %d", hits.Load())
	})
	c := NewResponseCache(10, time.Minute, false)
	h := c.Middleware(upstream)

	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, httptest.NewRequest("GET", "/x", nil))
	if w1.Body.String() != "hi 1" {
		t.Fatalf("first body = %q", w1.Body.String())
	}
	if w1.Header().Get("X-Cache") != "" {
		t.Errorf("first call should not be HIT")
	}

	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, httptest.NewRequest("GET", "/x", nil))
	if w2.Body.String() != "hi 1" {
		t.Errorf("second body = %q (cache should have served the original)", w2.Body.String())
	}
	if w2.Header().Get("X-Cache") != "HIT" {
		t.Errorf("expected X-Cache: HIT, got %q", w2.Header().Get("X-Cache"))
	}
	if hits.Load() != 1 {
		t.Errorf("upstream hit count = %d", hits.Load())
	}
}

func TestCacheTTLExpiry(t *testing.T) {
	var hits atomic.Int64
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprintln(w, "ok")
	})
	c := NewResponseCache(10, 30*time.Millisecond, false)
	h := c.Middleware(upstream)

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	}
	if hits.Load() != 1 {
		t.Errorf("hits before expiry = %d", hits.Load())
	}
	time.Sleep(50 * time.Millisecond)

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	if hits.Load() != 2 {
		t.Errorf("hits after expiry = %d, want 2", hits.Load())
	}
}

func TestCacheSkipsNonGet(t *testing.T) {
	var hits atomic.Int64
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
	})
	c := NewResponseCache(10, time.Minute, false)
	h := c.Middleware(upstream)

	for i := 0; i < 3; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", "/x", nil))
	}
	if hits.Load() != 3 {
		t.Errorf("POST should bypass cache, hits=%d", hits.Load())
	}
}

func TestCacheSkipsErrorResponses(t *testing.T) {
	var hits atomic.Int64
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		http.Error(w, "boom", 500)
	})
	c := NewResponseCache(10, time.Minute, false)
	h := c.Middleware(upstream)

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	}
	if hits.Load() != 2 {
		t.Errorf("5xx should not cache, hits=%d", hits.Load())
	}
}

func TestCacheRespectsNoStore(t *testing.T) {
	var hits atomic.Int64
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Cache-Control", "no-store")
		w.Write([]byte("hi"))
	})
	c := NewResponseCache(10, time.Minute, false)
	h := c.Middleware(upstream)

	for i := 0; i < 2; i++ {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "/x", nil))
	}
	if hits.Load() != 2 {
		t.Errorf("no-store should bypass cache, hits=%d", hits.Load())
	}
}

func TestCacheLRUEviction(t *testing.T) {
	var hits atomic.Int64
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, r.URL.Path)
	})
	c := NewResponseCache(2, time.Minute, false)
	h := c.Middleware(upstream)

	// Fill capacity with /a, /b.
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/a", nil))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/b", nil))
	// Touch /a so /b becomes least-recently-used.
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/a", nil))
	// /c should evict /b, not /a.
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/c", nil))

	preHits := hits.Load()
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/a", nil))
	if hits.Load() != preHits {
		t.Error("/a should still be cached")
	}
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/b", nil))
	if hits.Load() != preHits+1 {
		t.Error("/b should have been evicted")
	}
}

func TestCacheKeyByAuth(t *testing.T) {
	var hits atomic.Int64
	upstream := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		fmt.Fprint(w, r.Header.Get("Authorization"))
	})
	c := NewResponseCache(10, time.Minute, true)
	h := c.Middleware(upstream)

	r1 := httptest.NewRequest("GET", "/x", nil)
	r1.Header.Set("Authorization", "Bearer alice")
	w1 := httptest.NewRecorder()
	h.ServeHTTP(w1, r1)

	r2 := httptest.NewRequest("GET", "/x", nil)
	r2.Header.Set("Authorization", "Bearer bob")
	w2 := httptest.NewRecorder()
	h.ServeHTTP(w2, r2)

	if hits.Load() != 2 {
		t.Errorf("different auth headers should miss cache, hits=%d", hits.Load())
	}
	if w1.Body.String() != "Bearer alice" || w2.Body.String() != "Bearer bob" {
		t.Errorf("user-specific bodies not preserved: %q / %q", w1.Body.String(), w2.Body.String())
	}
}
