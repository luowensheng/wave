package http

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
)

// ── request ID ────────────────────────────────────────────────────────────

type ctxKey int

const reqIDKey ctxKey = 1

// RequestIDMiddleware ensures every request has an X-Request-Id header,
// echoing the one the client sent if present, otherwise generating a new
// 16-byte hex token. The ID is also placed on the request context so
// downstream handlers and structured loggers can pick it up.
func RequestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-Id")
		if id == "" {
			var b [16]byte
			_, _ = rand.Read(b[:])
			id = hex.EncodeToString(b[:])
		}
		w.Header().Set("X-Request-Id", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), reqIDKey, id)))
	})
}

// RequestIDFrom returns the request ID stashed by RequestIDMiddleware,
// or "" if the middleware was not in the chain.
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(reqIDKey).(string); ok {
		return v
	}
	return ""
}

// ── security headers ──────────────────────────────────────────────────────

// SecurityHeadersConfig controls which OWASP-recommended response headers
// are sent. Defaults are conservative: HSTS off (only safe over real TLS),
// CSP off (apps must opt in), but X-Content-Type-Options, X-Frame-Options,
// Referrer-Policy, Permissions-Policy on.
type SecurityHeadersConfig struct {
	HSTS              string // e.g. "max-age=31536000; includeSubDomains"
	CSP               string // e.g. "default-src 'self'"
	FrameOptions      string // default "DENY"
	ContentTypeNoSniff bool  // default true
	ReferrerPolicy    string // default "strict-origin-when-cross-origin"
	PermissionsPolicy string // default "interest-cohort=()"
}

// SecurityHeadersMiddleware sets sane defaults that can be overridden via
// SecurityHeadersConfig fields. Headers added pre-handler so handlers can
// still override them by Set().
func SecurityHeadersMiddleware(cfg SecurityHeadersConfig) func(http.Handler) http.Handler {
	if cfg.FrameOptions == "" {
		cfg.FrameOptions = "DENY"
	}
	if cfg.ReferrerPolicy == "" {
		cfg.ReferrerPolicy = "strict-origin-when-cross-origin"
	}
	if cfg.PermissionsPolicy == "" {
		cfg.PermissionsPolicy = "interest-cohort=()"
	}
	cfg.ContentTypeNoSniff = true

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			h := w.Header()
			if cfg.HSTS != "" && r.TLS != nil {
				h.Set("Strict-Transport-Security", cfg.HSTS)
			}
			if cfg.CSP != "" {
				h.Set("Content-Security-Policy", cfg.CSP)
			}
			h.Set("X-Frame-Options", cfg.FrameOptions)
			if cfg.ContentTypeNoSniff {
				h.Set("X-Content-Type-Options", "nosniff")
			}
			h.Set("Referrer-Policy", cfg.ReferrerPolicy)
			h.Set("Permissions-Policy", cfg.PermissionsPolicy)
			next.ServeHTTP(w, r)
		})
	}
}

// ── body size limit ───────────────────────────────────────────────────────

// MaxBodyMiddleware caps r.Body at maxBytes. Routes that need streaming
// uploads (file_server) should set their own limit instead of relying on
// this global cap. Default if maxBytes <= 0 is 16 MiB.
func MaxBodyMiddleware(maxBytes int64) func(http.Handler) http.Handler {
	if maxBytes <= 0 {
		maxBytes = 16 << 20
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > maxBytes {
				http.Error(w, "request entity too large", http.StatusRequestEntityTooLarge)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, maxBytes)
			next.ServeHTTP(w, r)
		})
	}
}

// ── chain helper ──────────────────────────────────────────────────────────

// Chain composes middleware in source order: Chain(a,b,c)(h) == a(b(c(h))).
func Chain(mw ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		for i := len(mw) - 1; i >= 0; i-- {
			if mw[i] == nil {
				continue
			}
			h = mw[i](h)
		}
		return h
	}
}

// SplitCSV splits a comma-separated header into trimmed fields.
// Used by per-route CORS handlers; lives here to avoid duplication.
func SplitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}
