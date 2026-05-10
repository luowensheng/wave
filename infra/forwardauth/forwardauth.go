// Package forwardauth implements the Traefik / nginx auth_request /
// Caddy forward_auth pattern: before each request reaches the
// underlying handler we make a sub-request to an external auth
// service. If it returns 2xx the request continues (with optional
// headers from the auth response copied onto it); anything else short-
// circuits with 401 (or the upstream's status).
//
// Useful when you already run a real authentication service (Authelia,
// Authentik, oauth2-proxy) and want wave to delegate to it
// instead of re-implementing OIDC client behavior.
package forwardauth

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Config drives one Verifier instance.
type Config struct {
	URL              string        // auth service endpoint
	Method           string        // default "GET"
	Timeout          time.Duration // default 5s
	ForwardHeaders   []string      // headers to copy from the original request to the sub-request
	ResponseHeaders  []string      // headers to copy from the auth response onto the original request
	TrustForwardedFor bool         // if true, forward X-Forwarded-{For,Proto,Host,Method,Uri}
}

// Verifier holds a single auth-service config.
type Verifier struct {
	cfg    Config
	client *http.Client
}

// New constructs a Verifier; URL is required.
func New(cfg Config) (*Verifier, error) {
	if cfg.URL == "" {
		return nil, fmt.Errorf("forwardauth: URL required")
	}
	if cfg.Method == "" {
		cfg.Method = http.MethodGet
	}
	if cfg.Timeout == 0 {
		cfg.Timeout = 5 * time.Second
	}
	return &Verifier{cfg: cfg, client: &http.Client{
		Timeout: cfg.Timeout,
		// Don't auto-follow redirects — the point is to *mirror* the
		// auth service's 302 (typical for OAuth login flows) back to the
		// browser. Default Go behavior would chase it and bury the
		// Location header.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}}, nil
}

// Middleware wraps next with the auth check. On 2xx from the auth
// service, ResponseHeaders are copied onto r before next.ServeHTTP. On
// anything else, the auth service's status + body are echoed to the
// client (so the auth service can redirect for OAuth flows etc.).
func (v *Verifier) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), v.cfg.Timeout)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, v.cfg.Method, v.cfg.URL, nil)
		if err != nil {
			http.Error(w, "auth service request build failed", http.StatusInternalServerError)
			return
		}
		// Forward the standard auth headers and any user-listed extras.
		for _, h := range v.cfg.ForwardHeaders {
			if val := r.Header.Get(h); val != "" {
				req.Header.Set(h, val)
			}
		}
		// Always forward Cookie + Authorization (the most useful defaults).
		if c := r.Header.Get("Authorization"); c != "" {
			req.Header.Set("Authorization", c)
		}
		if c := r.Header.Get("Cookie"); c != "" {
			req.Header.Set("Cookie", c)
		}
		if v.cfg.TrustForwardedFor {
			req.Header.Set("X-Forwarded-Method", r.Method)
			req.Header.Set("X-Forwarded-Uri", r.URL.RequestURI())
			req.Header.Set("X-Forwarded-Host", r.Host)
			proto := "http"
			if r.TLS != nil {
				proto = "https"
			}
			req.Header.Set("X-Forwarded-Proto", proto)
			if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
				req.Header.Set("X-Forwarded-For", xff)
			}
		}

		resp, err := v.client.Do(req)
		if err != nil {
			http.Error(w, "auth service unreachable", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			for _, h := range v.cfg.ResponseHeaders {
				if val := resp.Header.Get(h); val != "" {
					r.Header.Set(h, val)
				}
			}
			next.ServeHTTP(w, r)
			return
		}
		// Mirror the auth service's failure response 1:1 so OAuth
		// redirects and CSRF tokens propagate to the browser.
		for k, vs := range resp.Header {
			if shouldHopHeader(k) {
				continue
			}
			for _, val := range vs {
				w.Header().Add(k, val)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	})
}

func shouldHopHeader(name string) bool {
	switch strings.ToLower(name) {
	case "connection", "keep-alive", "transfer-encoding", "te", "trailer", "upgrade", "proxy-authorization", "proxy-authenticate":
		return true
	}
	return false
}
