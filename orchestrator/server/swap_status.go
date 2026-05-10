package servers

import (
	"bytes"
	"net/http"
)

// swapStatus wraps `next` so that any response with the given status
// code is replaced by the OnFail handler. Used to attach `limits:`
// fail actions onto middlewares that don't natively expose an
// OnFail hook (auth.RequireAuth, rbac.Middleware).
//
// The interceptor buffers headers + body; if the captured status
// matches, OnFail is invoked instead of flushing. Other statuses pass
// through untouched.
//
// Streaming opt-out: if the wrapped handler calls Flush before its
// final WriteHeader, we go into passthrough mode (no swap possible
// once bytes are on the wire).
func swapStatus(next http.HandlerFunc, status int, onFail http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bw := &swapWriter{ResponseWriter: w, code: 200}
		next(bw, r)
		if bw.flushed {
			return
		}
		if bw.code == status {
			onFail(w, r)
			return
		}
		// Not matching — replay the buffered response verbatim.
		w.WriteHeader(bw.code)
		if bw.buf.Len() > 0 {
			_, _ = w.Write(bw.buf.Bytes())
		}
	})
}

type swapWriter struct {
	http.ResponseWriter
	code     int
	buf      bytes.Buffer
	flushed  bool
	wroteHdr bool
}

func (s *swapWriter) Header() http.Header { return s.ResponseWriter.Header() }

func (s *swapWriter) WriteHeader(c int) {
	if s.flushed {
		s.ResponseWriter.WriteHeader(c)
		return
	}
	if s.wroteHdr {
		return
	}
	s.code = c
	s.wroteHdr = true
}

func (s *swapWriter) Write(p []byte) (int, error) {
	if s.flushed {
		return s.ResponseWriter.Write(p)
	}
	return s.buf.Write(p)
}

func (s *swapWriter) Flush() {
	if !s.flushed {
		s.flushed = true
		s.ResponseWriter.WriteHeader(s.code)
		if s.buf.Len() > 0 {
			_, _ = s.ResponseWriter.Write(s.buf.Bytes())
			s.buf.Reset()
		}
	}
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
