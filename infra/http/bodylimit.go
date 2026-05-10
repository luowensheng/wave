package http

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// BodyLimitConfig drives the per-route max-body middleware. When the
// request body exceeds MaxBytes, exactly one of the action fields
// determines the response. Default action (none set) is 413 + JSON.
//
// The size limit is enforced both via Content-Length (early reject)
// and via a wrapping http.MaxBytesReader so streaming uploads also
// trip on overflow.
type BodyLimitConfig struct {
	MaxBytes int64

	// Action — at most one should be set; first non-empty wins.
	Redirect       string // 302 to this URL
	TemplateFile   string // file path; rendered as text/html
	TemplateInline string // inline HTML/text body

	// StatusOverride lets callers send (say) 400 instead of 413. Only
	// applied when no Redirect was specified. Default 413.
	StatusOverride int

	// OnFail, when non-nil, takes over the response. Used by the
	// orchestrator's `limits:` block to share a single render strategy
	// (redirect / template_inline / template_file / route_path)
	// across every middleware. When set, the inline Redirect /
	// TemplateInline / TemplateFile / StatusOverride fields are ignored.
	OnFail http.HandlerFunc

	// OnExceeded is an optional hook fired when the limit trips, for
	// audit / metrics. Receives the request's Content-Length (-1 if
	// unknown — i.e. streaming overflow).
	OnExceeded func(r *http.Request, contentLength int64)
}

// BodyLimitMiddleware enforces cfg. Wraps every request method (not
// only POST/PUT/PATCH) so HEAD-with-body-style attacks fail too.
func BodyLimitMiddleware(cfg BodyLimitConfig) func(http.Handler) http.Handler {
	if cfg.MaxBytes <= 0 {
		return func(next http.Handler) http.Handler { return next }
	}
	tplBody := loadTemplateBody(cfg)
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > cfg.MaxBytes {
				if cfg.OnExceeded != nil {
					cfg.OnExceeded(r, r.ContentLength)
				}
				if cfg.OnFail != nil {
					cfg.OnFail(w, r)
				} else {
					writeTooLarge(w, r, cfg, tplBody, cfg.MaxBytes)
				}
				return
			}
			// Wrap the body so streaming reads also trip. We can't tell
			// the inner handler which action to take, so we sniff for the
			// MaxBytesError on its way out via a tripping reader.
			r.Body = &trippingReader{
				ReadCloser: http.MaxBytesReader(w, r.Body, cfg.MaxBytes),
				cfg:        cfg, tplBody: tplBody, w: w, r: r,
			}
			next.ServeHTTP(w, r)
		})
	}
}

// trippingReader detects http.MaxBytesError on Read and writes the
// configured response *before* the inner handler can write its own
// raw 400/413. Marks tripped so the inner handler's writes become
// no-ops.
type trippingReader struct {
	io.ReadCloser
	cfg     BodyLimitConfig
	tplBody []byte
	w       http.ResponseWriter
	r       *http.Request
	tripped bool
}

func (t *trippingReader) Read(p []byte) (int, error) {
	if t.tripped {
		return 0, fmt.Errorf("body too large")
	}
	n, err := t.ReadCloser.Read(p)
	if err != nil {
		// Match either the typed error or the legacy string for older Go.
		if isMaxBytesErr(err) {
			t.tripped = true
			if t.cfg.OnExceeded != nil {
				t.cfg.OnExceeded(t.r, -1)
			}
			if t.cfg.OnFail != nil {
				t.cfg.OnFail(t.w, t.r)
			} else {
				writeTooLarge(t.w, t.r, t.cfg, t.tplBody, t.cfg.MaxBytes)
			}
		}
	}
	return n, err
}

func isMaxBytesErr(err error) bool {
	if err == nil {
		return false
	}
	if _, ok := err.(*http.MaxBytesError); ok {
		return true
	}
	return strings.Contains(err.Error(), "http: request body too large")
}

func writeTooLarge(w http.ResponseWriter, r *http.Request, cfg BodyLimitConfig, tplBody []byte, max int64) {
	if cfg.Redirect != "" {
		http.Redirect(w, r, cfg.Redirect, http.StatusFound)
		return
	}
	status := cfg.StatusOverride
	if status == 0 {
		status = http.StatusRequestEntityTooLarge
	}
	if len(tplBody) > 0 {
		ct := "text/html; charset=utf-8"
		if !looksHTML(tplBody) {
			ct = "text/plain; charset=utf-8"
		}
		w.Header().Set("Content-Type", ct)
		w.WriteHeader(status)
		_, _ = w.Write(tplBody)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = fmt.Fprintf(w, `{"error":"request body too large","limit_bytes":%d}`, max)
}

func loadTemplateBody(cfg BodyLimitConfig) []byte {
	if cfg.TemplateInline != "" {
		return []byte(cfg.TemplateInline)
	}
	if cfg.TemplateFile != "" {
		b, err := os.ReadFile(cfg.TemplateFile)
		if err == nil {
			return b
		}
	}
	return nil
}

func looksHTML(b []byte) bool {
	s := strings.TrimSpace(string(b))
	return strings.HasPrefix(s, "<")
}

// ParseBytesString accepts "5MB", "1.5GB", "200kb", "1024", etc.
// Returns the byte count or an error.
func ParseBytesString(s string) (int64, error) {
	s = strings.TrimSpace(strings.ToUpper(s))
	if s == "" {
		return 0, fmt.Errorf("empty size")
	}
	// Strip trailing B for "5MB" / "1KB" — lone "5M" also works.
	if strings.HasSuffix(s, "B") {
		s = strings.TrimSuffix(s, "B")
	}
	mult := int64(1)
	switch {
	case strings.HasSuffix(s, "K"):
		mult = 1 << 10
		s = strings.TrimSuffix(s, "K")
	case strings.HasSuffix(s, "M"):
		mult = 1 << 20
		s = strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "G"):
		mult = 1 << 30
		s = strings.TrimSuffix(s, "G")
	case strings.HasSuffix(s, "T"):
		mult = 1 << 40
		s = strings.TrimSuffix(s, "T")
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0, fmt.Errorf("invalid size %q", s)
	}
	if v < 0 {
		return 0, fmt.Errorf("negative size")
	}
	return int64(v * float64(mult)), nil
}
