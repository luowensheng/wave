package http

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"strconv"
	"strings"
)

// ETagMiddleware buffers safe-method response bodies, hashes them into a
// strong ETag, and returns 304 Not Modified when the request's
// If-None-Match matches. Skipped on streaming responses (any handler that
// calls Flush is opted out automatically — buffering would defeat the
// streaming).
//
// Use this around handlers whose responses are deterministic for the
// request: storage_access reads, file serving, /openapi.json,
// /api/streams.json, /metrics. Don't wrap mutation routes — pointless
// overhead.
func ETagMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}
		bw := &bufferingWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(bw, r)

		if bw.flushed || bw.status >= 500 {
			// Already streamed (Flush called) or upstream errored —
			// don't try to add an ETag. Buffered bytes (if any) were
			// already written when Flush ran.
			return
		}

		body := bw.buf.Bytes()
		etag := `"` + hex.EncodeToString(sha256.New().Sum(body))[:32] + `"`
		w.Header().Set("ETag", etag)

		if matchesIfNoneMatch(r.Header.Get("If-None-Match"), etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}

		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		w.WriteHeader(bw.status)
		_, _ = w.Write(body)
	})
}

// bufferingWriter captures status + body until either Flush is called
// (streaming opt-out) or the handler returns (then ETag layer takes over).
type bufferingWriter struct {
	http.ResponseWriter
	buf     bytes.Buffer
	status  int
	flushed bool
	wroteH  bool
}

func (b *bufferingWriter) Header() http.Header { return b.ResponseWriter.Header() }

func (b *bufferingWriter) WriteHeader(code int) {
	if b.flushed {
		b.ResponseWriter.WriteHeader(code)
		return
	}
	b.status = code
	b.wroteH = true
}

func (b *bufferingWriter) Write(p []byte) (int, error) {
	if b.flushed {
		return b.ResponseWriter.Write(p)
	}
	return b.buf.Write(p)
}

func (b *bufferingWriter) Flush() {
	if !b.flushed {
		b.flushed = true
		// Promote whatever we buffered to the wire.
		if !b.wroteH {
			b.wroteH = true
		}
		b.ResponseWriter.WriteHeader(b.status)
		if b.buf.Len() > 0 {
			_, _ = b.ResponseWriter.Write(b.buf.Bytes())
			b.buf.Reset()
		}
	}
	if f, ok := b.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// matchesIfNoneMatch handles the * wildcard and comma-separated lists.
func matchesIfNoneMatch(header, etag string) bool {
	header = strings.TrimSpace(header)
	if header == "" {
		return false
	}
	if header == "*" {
		return true
	}
	for _, candidate := range strings.Split(header, ",") {
		c := strings.TrimSpace(candidate)
		// Strip the W/ weak-validator prefix per RFC 7232.
		if strings.HasPrefix(c, "W/") {
			c = c[2:]
		}
		if c == etag {
			return true
		}
	}
	return false
}
