package http

import (
	"compress/gzip"
	"io"
	"net/http"
	"strings"
	"sync"
)

// GzipMiddleware compresses responses when the client advertises gzip
// support via Accept-Encoding. Skips:
//   - Streaming responses (handler called Flush before any write)
//   - Responses with an existing Content-Encoding
//   - Tiny bodies (<512B) where the framing overhead would dominate
//   - SSE / WebSocket / event-stream content types
//
// Sets `Content-Encoding: gzip` and `Vary: Accept-Encoding` for any
// successful compression.
func GzipMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !acceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}
		gw := newGzipWriter(w)
		defer gw.Close()
		next.ServeHTTP(gw, r)
	})
}

func acceptsGzip(r *http.Request) bool {
	for _, enc := range strings.Split(r.Header.Get("Accept-Encoding"), ",") {
		if strings.TrimSpace(strings.SplitN(enc, ";", 2)[0]) == "gzip" {
			return true
		}
	}
	return false
}

// gzipWriter buffers the first chunk of bytes to make a streaming /
// content-type / size decision. After that decision it either writes
// directly to the underlying ResponseWriter (passthrough) or to a
// gzip.Writer (compression).
type gzipWriter struct {
	http.ResponseWriter
	buf         []byte
	wroteHeader bool
	status      int
	gz          *gzip.Writer
	passthrough bool
	flushed     bool
}

const minCompressBytes = 512

var gzPool = sync.Pool{
	New: func() any { return gzip.NewWriter(io.Discard) },
}

func newGzipWriter(w http.ResponseWriter) *gzipWriter {
	return &gzipWriter{ResponseWriter: w, status: http.StatusOK}
}

func (g *gzipWriter) WriteHeader(c int) {
	if g.wroteHeader {
		return
	}
	g.status = c
	// We can't write the header yet — we don't know if we're compressing.
	// Defer until first Write or Close.
}

func (g *gzipWriter) Write(p []byte) (int, error) {
	// Streaming opt-out: a handler that calls Flush wants bytes on the
	// wire immediately, which gzip buffering breaks. We respect that by
	// going passthrough if Flush was called before our first write.
	if g.flushed && g.gz == nil {
		g.commitPassthrough()
	}
	if g.passthrough {
		return g.ResponseWriter.Write(p)
	}
	if g.gz != nil {
		return g.gz.Write(p)
	}

	g.buf = append(g.buf, p...)
	if len(g.buf) < minCompressBytes {
		return len(p), nil
	}
	// Decide.
	if g.shouldCompress() {
		g.commitCompressed()
		return len(p), nil
	}
	g.commitPassthrough()
	return len(p), nil
}

func (g *gzipWriter) shouldCompress() bool {
	ct := g.Header().Get("Content-Type")
	if ct == "" {
		// Don't gzip when we can't tell — could be binary.
		return false
	}
	if strings.Contains(ct, "text/event-stream") || strings.Contains(ct, "image/") || strings.Contains(ct, "video/") {
		return false
	}
	if g.Header().Get("Content-Encoding") != "" {
		return false
	}
	return true
}

func (g *gzipWriter) commitCompressed() {
	g.Header().Set("Content-Encoding", "gzip")
	g.Header().Add("Vary", "Accept-Encoding")
	g.Header().Del("Content-Length")
	g.wroteHeader = true
	g.ResponseWriter.WriteHeader(g.status)

	gz := gzPool.Get().(*gzip.Writer)
	gz.Reset(g.ResponseWriter)
	g.gz = gz
	if len(g.buf) > 0 {
		_, _ = g.gz.Write(g.buf)
		g.buf = nil
	}
}

func (g *gzipWriter) commitPassthrough() {
	g.passthrough = true
	g.wroteHeader = true
	g.ResponseWriter.WriteHeader(g.status)
	if len(g.buf) > 0 {
		_, _ = g.ResponseWriter.Write(g.buf)
		g.buf = nil
	}
}

// Close flushes any pending bytes / closes the gzip stream.
func (g *gzipWriter) Close() {
	if g.gz != nil {
		_ = g.gz.Close()
		gzPool.Put(g.gz)
		g.gz = nil
		return
	}
	if !g.wroteHeader {
		// Body never grew large enough to trigger a decision.
		g.commitPassthrough()
	}
}

// Flush implements http.Flusher with the streaming opt-out described
// above.
func (g *gzipWriter) Flush() {
	if g.gz != nil {
		_ = g.gz.Flush()
	}
	g.flushed = true
	if !g.wroteHeader {
		g.commitPassthrough()
	}
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
