package servers

import (
	"net/http"

	"github.com/luowensheng/wave/infra/connections"
)

// corsResponseWriter intercepts WriteHeader so the configured CORS
// allow-origin headers win over anything the inner handler (or proxy
// upstream) wrote. Without this, a route that proxies to an upstream
// which itself emits CORS produces duplicate
// Access-Control-Allow-Origin headers — Chrome rejects those.
//
// Use:
//
//	cw := newCORSResponseWriter(w, r, origins)
//	innerHandler.ServeHTTP(cw, r)
//	cw.commitHeaders()  // ensures even handlers that never call
//	                    // WriteHeader still get CORS applied
type corsResponseWriter struct {
	http.ResponseWriter
	r           *http.Request
	origins     []string
	wroteHeader bool
}

func newCORSResponseWriter(w http.ResponseWriter, r *http.Request, origins []string) *corsResponseWriter {
	return &corsResponseWriter{ResponseWriter: w, r: r, origins: origins}
}

func (c *corsResponseWriter) WriteHeader(code int) {
	if c.wroteHeader {
		return
	}
	c.wroteHeader = true
	c.applyCORS()
	c.ResponseWriter.WriteHeader(code)
}

func (c *corsResponseWriter) Write(p []byte) (int, error) {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	return c.ResponseWriter.Write(p)
}

// commitHeaders is a safety net for handlers that finish without ever
// calling WriteHeader (a no-content reply, or a panic before write).
// Idempotent — does nothing once the real WriteHeader has fired.
func (c *corsResponseWriter) commitHeaders() {
	if c.wroteHeader {
		return
	}
	c.applyCORS()
}

// Flush passes through to the underlying writer, preserving SSE /
// chunked-streaming behavior on routes that have CORS configured.
func (c *corsResponseWriter) Flush() {
	if !c.wroteHeader {
		c.WriteHeader(http.StatusOK)
	}
	if f, ok := c.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (c *corsResponseWriter) applyCORS() {
	// Strip any upstream-supplied CORS headers so our values win
	// instead of accumulating.
	h := c.ResponseWriter.Header()
	h.Del("Access-Control-Allow-Origin")
	h.Del("Access-Control-Allow-Credentials")
	h.Del("Access-Control-Expose-Headers")
	// Vary is additive (multiple values for different request headers
	// are legal), so let HandleCORS Set it freshly without strip.
	connections.HandleCORS(c.ResponseWriter, c.r, c.origins)
}
