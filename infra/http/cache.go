package http

import (
	"container/list"
	"net/http"
	"sync"
	"time"
)

// ResponseCache is an in-memory LRU response cache with TTL. Designed
// for GET responses on routes whose output is determined by URL +
// (optionally) the Authorization header. Wraps a handler; on cache hit
// short-circuits with the previously-served bytes + headers.
//
// Bounds: max N entries (LRU-evicted) and per-entry TTL. No request
// body / non-GET caching — keeps invalidation impossible to mess up.
type ResponseCache struct {
	max     int
	ttl     time.Duration
	keyAuth bool

	mu    sync.Mutex
	items map[string]*list.Element
	order *list.List
}

type cacheEntry struct {
	key       string
	headers   http.Header
	status    int
	body      []byte
	expires   time.Time
}

// NewResponseCache creates a cache holding up to maxEntries items with
// the given TTL. keyByAuth=true folds the Authorization header into the
// cache key so cached results don't leak across users.
func NewResponseCache(maxEntries int, ttl time.Duration, keyByAuth bool) *ResponseCache {
	if maxEntries <= 0 {
		maxEntries = 1024
	}
	if ttl <= 0 {
		ttl = time.Minute
	}
	return &ResponseCache{
		max:     maxEntries,
		ttl:     ttl,
		keyAuth: keyByAuth,
		items:   make(map[string]*list.Element, maxEntries),
		order:   list.New(),
	}
}

// Middleware wraps a handler with cache lookup + populate. Skips
// non-GET, skips responses with status >= 400 (don't cache errors), and
// honors Cache-Control: no-store on the upstream response.
func (c *ResponseCache) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}
		key := c.keyFor(r)

		if e, ok := c.lookup(key); ok {
			for k, vs := range e.headers {
				for _, v := range vs {
					w.Header().Add(k, v)
				}
			}
			w.Header().Set("X-Cache", "HIT")
			w.WriteHeader(e.status)
			_, _ = w.Write(e.body)
			return
		}

		cw := &captureWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(cw, r)

		// Populate cache only for cacheable responses.
		if cw.status >= 200 && cw.status < 400 &&
			!noStore(cw.Header()) {
			c.store(key, &cacheEntry{
				key:     key,
				headers: cw.Header().Clone(),
				status:  cw.status,
				body:    cw.buf,
				expires: time.Now().Add(c.ttl),
			})
		}
	})
}

func (c *ResponseCache) keyFor(r *http.Request) string {
	k := r.URL.Path + "?" + r.URL.RawQuery
	if c.keyAuth {
		k += "|" + r.Header.Get("Authorization")
	}
	return k
}

func (c *ResponseCache) lookup(key string) (*cacheEntry, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	el, ok := c.items[key]
	if !ok {
		return nil, false
	}
	e := el.Value.(*cacheEntry)
	if time.Now().After(e.expires) {
		c.order.Remove(el)
		delete(c.items, key)
		return nil, false
	}
	c.order.MoveToFront(el)
	return e, true
}

func (c *ResponseCache) store(key string, e *cacheEntry) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if existing, ok := c.items[key]; ok {
		existing.Value = e
		c.order.MoveToFront(existing)
		return
	}
	el := c.order.PushFront(e)
	c.items[key] = el
	for c.order.Len() > c.max {
		oldest := c.order.Back()
		if oldest == nil {
			break
		}
		c.order.Remove(oldest)
		delete(c.items, oldest.Value.(*cacheEntry).key)
	}
}

// Stats exposes basic cardinality for /metrics or admin.
func (c *ResponseCache) Stats() (entries, capacity int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len(), c.max
}

// captureWriter buffers status + body for cache populate. We don't try
// to be clever about streaming responses — handlers that call Flush
// immediately push bytes through and the cache populate becomes a no-op
// because writes happened on the wrapped writer (cw.buf still has them
// duplicated, which is wasteful but harmless; alternative would be a
// flushed-flag like ETag). For now keep it simple.
type captureWriter struct {
	http.ResponseWriter
	status int
	buf    []byte
}

func (cw *captureWriter) WriteHeader(code int) {
	cw.status = code
	cw.ResponseWriter.WriteHeader(code)
}

func (cw *captureWriter) Write(p []byte) (int, error) {
	cw.buf = append(cw.buf, p...)
	return cw.ResponseWriter.Write(p)
}

func (cw *captureWriter) Flush() {
	if f, ok := cw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func noStore(h http.Header) bool {
	cc := h.Get("Cache-Control")
	if cc == "" {
		return false
	}
	for _, part := range splitComma(cc) {
		if part == "no-store" || part == "private" {
			return true
		}
	}
	return false
}
