package http

import (
	"net"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// TokenBucket is a per-key fixed-rate limiter (rps tokens per second,
// burst max tokens). Unused buckets are GC'd after ~10 minutes of
// inactivity to keep memory bounded. Designed for per-IP or per-route
// use; cardinality is bounded by your traffic source set.
type TokenBucket struct {
	rps   float64
	burst float64

	mu      sync.Mutex
	buckets map[string]*bucketState
}

type bucketState struct {
	tokens   float64
	last     time.Time
	lastUsed time.Time
}

func NewTokenBucket(rps, burst float64) *TokenBucket {
	if burst <= 0 {
		burst = rps
	}
	tb := &TokenBucket{rps: rps, burst: burst, buckets: make(map[string]*bucketState)}
	go tb.gcLoop()
	return tb
}

// Allow consumes one token for key; returns false if depleted.
func (t *TokenBucket) Allow(key string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	b, ok := t.buckets[key]
	if !ok {
		b = &bucketState{tokens: t.burst, last: now, lastUsed: now}
		t.buckets[key] = b
	}
	elapsed := now.Sub(b.last).Seconds()
	b.tokens += elapsed * t.rps
	if b.tokens > t.burst {
		b.tokens = t.burst
	}
	b.last = now
	b.lastUsed = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func (t *TokenBucket) gcLoop() {
	tick := time.NewTicker(time.Minute)
	defer tick.Stop()
	for range tick.C {
		t.mu.Lock()
		cutoff := time.Now().Add(-10 * time.Minute)
		for k, b := range t.buckets {
			if b.lastUsed.Before(cutoff) {
				delete(t.buckets, k)
			}
		}
		t.mu.Unlock()
	}
}

// Middleware applies the limiter using the supplied keyFn (typically
// `ClientIP`). On rejection writes 429 with a Retry-After header.
func (t *TokenBucket) Middleware(keyFn func(*http.Request) string) func(http.Handler) http.Handler {
	return t.MiddlewareWithFail(keyFn, nil)
}

// MiddlewareWithFail is like Middleware but lets the caller swap the
// 429 response for a custom handler — used by the orchestrator's
// `limits:` block to share one renderer across every middleware.
func (t *TokenBucket) MiddlewareWithFail(keyFn func(*http.Request) string, onFail http.HandlerFunc) func(http.Handler) http.Handler {
	if keyFn == nil {
		keyFn = ClientIP
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !t.Allow(keyFn(r)) {
				if onFail != nil {
					onFail(w, r)
					return
				}
				w.Header().Set("Retry-After", strconv.Itoa(int(1.0/t.rps)+1))
				http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ClientIP extracts a best-effort client IP from XFF / X-Real-IP /
// RemoteAddr. Used as the default rate-limiter key.
func ClientIP(r *http.Request) string {
	if v := r.Header.Get("X-Real-IP"); v != "" && net.ParseIP(v) != nil {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		for _, p := range splitComma(v) {
			if net.ParseIP(p) != nil {
				return p
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func splitComma(s string) []string {
	out := []string{}
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, trimSpace(cur))
			cur = ""
			continue
		}
		cur += string(r)
	}
	if cur != "" {
		out = append(out, trimSpace(cur))
	}
	return out
}

func trimSpace(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t') {
		s = s[1:]
	}
	for len(s) > 0 && (s[len(s)-1] == ' ' || s[len(s)-1] == '\t') {
		s = s[:len(s)-1]
	}
	return s
}
