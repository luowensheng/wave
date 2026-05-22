package servers

import (
	infrahttp "github.com/luowensheng/wave/infra/http"
	"net/http"
)

// loggingMiddleware wraps the infra implementation so existing callers in this
// package continue to compile without change.
func loggingMiddleware(next http.Handler) http.Handler {
	return infrahttp.LoggingMiddleware(next)
}

// statusCapturingWriter is a minimal http.ResponseWriter wrapper that
// records the status code so observability middleware can label
// metrics with the response status. Forwards Write/Flush as-is.
type statusCapturingWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusCapturingWriter) WriteHeader(code int) {
	if !s.wroteHeader {
		s.status = code
		s.wroteHeader = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusCapturingWriter) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.wroteHeader = true
		// status default already set on construction
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusCapturingWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
