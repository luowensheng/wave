package servers

import (
	"bytes"
	"net/http"
	"strconv"
)

// errorCaseMiddleware buffers the wrapped handler's response, inspects
// the captured status, and either flushes the buffered response
// verbatim (status doesn't match the entry's filter) OR drops it and
// invokes onFail (status matches).
//
// Filter precedence: explicit StatusCodes wins; otherwise the
// (StatusMin, StatusMax) inclusive range applies; if neither is set,
// every 4xx + 5xx response triggers (400..599).
//
// Streaming opt-out: if the inner handler calls Flush() before
// committing a status, we go into passthrough mode (the redirect /
// template path is no longer possible once bytes are on the wire).
func errorCaseMiddleware(entry *LimitEntry, onFail http.HandlerFunc) func(http.Handler) http.Handler {
	matches := errorMatcher(entry)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rw := &errorCaseWriter{ResponseWriter: w, status: http.StatusOK}
			next.ServeHTTP(rw, r)
			if rw.flushed {
				return
			}
			if matches(rw.status) {
				onFail(w, r)
				return
			}
			w.Header().Set("Content-Length", strconv.Itoa(rw.buf.Len()))
			w.WriteHeader(rw.status)
			if rw.buf.Len() > 0 {
				_, _ = w.Write(rw.buf.Bytes())
			}
		})
	}
}

func errorMatcher(e *LimitEntry) func(int) bool {
	if len(e.StatusCodes) > 0 {
		set := make(map[int]struct{}, len(e.StatusCodes))
		for _, c := range e.StatusCodes {
			set[c] = struct{}{}
		}
		return func(s int) bool { _, ok := set[s]; return ok }
	}
	min, max := e.StatusMin, e.StatusMax
	if min == 0 && max == 0 {
		min, max = 400, 599
	}
	return func(s int) bool { return s >= min && s <= max }
}

type errorCaseWriter struct {
	http.ResponseWriter
	status   int
	buf      bytes.Buffer
	flushed  bool
	headerOK bool
}

func (rw *errorCaseWriter) WriteHeader(code int) {
	if rw.flushed {
		rw.ResponseWriter.WriteHeader(code)
		return
	}
	if rw.headerOK {
		return
	}
	rw.status = code
	rw.headerOK = true
}

func (rw *errorCaseWriter) Write(p []byte) (int, error) {
	if rw.flushed {
		return rw.ResponseWriter.Write(p)
	}
	return rw.buf.Write(p)
}

func (rw *errorCaseWriter) Flush() {
	if !rw.flushed {
		rw.flushed = true
		rw.ResponseWriter.WriteHeader(rw.status)
		if rw.buf.Len() > 0 {
			_, _ = rw.ResponseWriter.Write(rw.buf.Bytes())
			rw.buf.Reset()
		}
	}
	if f, ok := rw.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
