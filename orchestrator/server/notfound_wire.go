package servers

import (
	"bytes"
	"fmt"
	"log"
	"net/http"
)

// registerNotFound installs a catch-all handler at "/" using whatever
// route type the user pointed `not_found:` at. Only runs when nothing
// more specific in the mux matched, per net/http.ServeMux semantics.
//
// Status convention: any inner handler that didn't explicitly write a
// status gets 404. Handlers that DID set a status (e.g. a plugin
// returning 200 with a help page) win — we don't override deliberate
// choices.
func (s *Server) registerNotFound(args map[string]string) error {
	if s.Config == nil || s.Config.NotFound == nil {
		return nil
	}
	nf := s.Config.NotFound
	// The Route was constructed via YAML; fill the inner config and
	// surface any errors at boot rather than first miss.
	if nf.Path == "" {
		nf.Path = "/" // satisfy Validate(); not actually used by the handler
	}
	if err := nf.setRouteConfig(); err != nil {
		return fmt.Errorf("not_found: %w", err)
	}
	if err := nf.Validate(); err != nil {
		return fmt.Errorf("not_found: %w", err)
	}
	inner, err := nf.config.CreateRoute(nf.Method, nf.Path, args)
	if err != nil {
		return fmt.Errorf("not_found CreateRoute: %w", err)
	}

	s.mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Buffer the inner response so we can default the status code
		// to 404 only when the inner handler didn't pick its own.
		bw := &notFoundBuffer{ResponseWriter: w, headerCode: 0}
		inner(bw, r)
		bw.flush()
	})
	log.Printf("registered not_found catch-all: type=%s", nf.Type)
	return nil
}

// notFoundBuffer captures status + body so we can apply the default
// 404 only when the inner handler didn't set its own.
type notFoundBuffer struct {
	http.ResponseWriter
	headerCode int
	body       bytes.Buffer
	headerSent bool
}

func (b *notFoundBuffer) Header() http.Header { return b.ResponseWriter.Header() }

func (b *notFoundBuffer) WriteHeader(code int) {
	if b.headerSent {
		return
	}
	b.headerCode = code
}

func (b *notFoundBuffer) Write(p []byte) (int, error) {
	return b.body.Write(p)
}

func (b *notFoundBuffer) flush() {
	code := b.headerCode
	if code == 0 {
		code = http.StatusNotFound
	}
	b.ResponseWriter.WriteHeader(code)
	if b.body.Len() > 0 {
		_, _ = b.ResponseWriter.Write(b.body.Bytes())
	}
	b.headerSent = true
}
