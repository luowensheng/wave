// Package webhooksig validates HMAC signatures on incoming webhooks
// (Stripe, GitHub, Slack, Twilio, generic). Designed to plug in as a
// per-route auth — we expose a Verifier interface plus a stdlib http
// middleware factory so callers can wire it however they want.
//
// Goals: zero deps; constant-time comparison; per-provider quirks
// (Stripe's `t=...,v1=...` envelope, GitHub's `sha256=...` prefix,
// Slack's `v0:<ts>:<body>` signing string) handled in one place.
package webhooksig

import (
	"crypto/hmac"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"hash"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Provider names supported out of the box.
const (
	ProviderStripe  = "stripe"
	ProviderGitHub  = "github"
	ProviderSlack   = "slack"
	ProviderGeneric = "generic" // Header value is `<algo>=<hex>`; algo is sha256 unless `algorithm` is set
)

// Config controls a Verifier instance.
type Config struct {
	Provider       string        // "stripe" | "github" | "slack" | "generic"
	Secret         string        // shared secret
	Header         string        // header carrying the signature; default per provider
	Tolerance      time.Duration // max age for timestamped providers; default 5m
	Algorithm      string        // generic only: "sha256" (default) | "sha1"
	HeaderPrefix   string        // generic only: e.g. "sha256=" — stripped before hex compare
	NowFn          func() time.Time
}

// Verifier reads an http.Request, returns nil on a valid signature or
// an explanatory error on failure. Reads (and replaces) r.Body.
type Verifier interface {
	Verify(r *http.Request) error
}

// New constructs a Verifier from the config. Secret must be non-empty.
func New(cfg Config) (Verifier, error) {
	if cfg.Secret == "" {
		return nil, fmt.Errorf("webhooksig: empty secret")
	}
	if cfg.NowFn == nil {
		cfg.NowFn = time.Now
	}
	if cfg.Tolerance == 0 {
		cfg.Tolerance = 5 * time.Minute
	}
	switch cfg.Provider {
	case ProviderStripe:
		if cfg.Header == "" {
			cfg.Header = "Stripe-Signature"
		}
		return &stripeVerifier{cfg: cfg}, nil
	case ProviderGitHub:
		if cfg.Header == "" {
			cfg.Header = "X-Hub-Signature-256"
		}
		return &githubVerifier{cfg: cfg}, nil
	case ProviderSlack:
		if cfg.Header == "" {
			cfg.Header = "X-Slack-Signature"
		}
		return &slackVerifier{cfg: cfg}, nil
	case ProviderGeneric, "":
		if cfg.Header == "" {
			cfg.Header = "X-Signature"
		}
		if cfg.Algorithm == "" {
			cfg.Algorithm = "sha256"
		}
		return &genericVerifier{cfg: cfg}, nil
	default:
		return nil, fmt.Errorf("webhooksig: unknown provider %q", cfg.Provider)
	}
}

// Middleware wraps a handler so requests with a missing or invalid
// signature get 401 (no body leak — sig failures are deliberate).
func Middleware(v Verifier) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if err := v.Verify(r); err != nil {
				http.Error(w, "invalid signature", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ── helpers ────────────────────────────────────────────────────────────────

// readAndRestore reads the entire body and puts a fresh ReadCloser back
// on r so the downstream handler can re-read.
func readAndRestore(r *http.Request) ([]byte, error) {
	if r.Body == nil {
		return nil, nil
	}
	b, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		return nil, err
	}
	r.Body = newRepeatReader(b)
	return b, nil
}

type repeatReader struct{ b []byte; i int }

func newRepeatReader(b []byte) io.ReadCloser { return &repeatReader{b: b} }

func (r *repeatReader) Read(p []byte) (int, error) {
	if r.i >= len(r.b) {
		return 0, io.EOF
	}
	n := copy(p, r.b[r.i:])
	r.i += n
	return n, nil
}
func (r *repeatReader) Close() error { return nil }

func newHMAC(algo, secret string) (hash.Hash, error) {
	switch strings.ToLower(algo) {
	case "sha256", "":
		return hmac.New(sha256.New, []byte(secret)), nil
	case "sha1":
		return hmac.New(sha1.New, []byte(secret)), nil
	default:
		return nil, fmt.Errorf("unsupported algorithm %q", algo)
	}
}

func hexEqual(a, b string) bool {
	ab, err := hex.DecodeString(a)
	if err != nil {
		return false
	}
	bb, err := hex.DecodeString(b)
	if err != nil {
		return false
	}
	return hmac.Equal(ab, bb)
}

// ── Stripe ────────────────────────────────────────────────────────────────

type stripeVerifier struct{ cfg Config }

// Stripe-Signature header form: t=1614010000,v1=hex,v1=hex2
// Signed string: "{t}.{body}" with sha256.
func (s *stripeVerifier) Verify(r *http.Request) error {
	header := r.Header.Get(s.cfg.Header)
	if header == "" {
		return fmt.Errorf("missing %s header", s.cfg.Header)
	}
	body, err := readAndRestore(r)
	if err != nil {
		return err
	}

	var ts string
	var sigs []string
	for _, p := range strings.Split(header, ",") {
		k, v, ok := strings.Cut(p, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		switch k {
		case "t":
			ts = v
		case "v1":
			sigs = append(sigs, v)
		}
	}
	if ts == "" || len(sigs) == 0 {
		return fmt.Errorf("malformed Stripe-Signature header")
	}

	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("bad timestamp")
	}
	if age := s.cfg.NowFn().Sub(time.Unix(tsInt, 0)); age > s.cfg.Tolerance || -age > s.cfg.Tolerance {
		return fmt.Errorf("timestamp outside tolerance (age=%s)", age)
	}

	mac := hmac.New(sha256.New, []byte(s.cfg.Secret))
	mac.Write([]byte(ts))
	mac.Write([]byte("."))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))

	for _, sig := range sigs {
		if hexEqual(sig, want) {
			return nil
		}
	}
	return fmt.Errorf("signature mismatch")
}

// ── GitHub ────────────────────────────────────────────────────────────────

type githubVerifier struct{ cfg Config }

// Header: X-Hub-Signature-256: sha256=<hex>
func (g *githubVerifier) Verify(r *http.Request) error {
	raw := r.Header.Get(g.cfg.Header)
	if raw == "" {
		return fmt.Errorf("missing %s header", g.cfg.Header)
	}
	parts := strings.SplitN(raw, "=", 2)
	if len(parts) != 2 || parts[0] != "sha256" {
		return fmt.Errorf("malformed signature header")
	}
	body, err := readAndRestore(r)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(g.cfg.Secret))
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hexEqual(parts[1], want) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

// ── Slack ─────────────────────────────────────────────────────────────────

type slackVerifier struct{ cfg Config }

// Slack signs sha256 of "v0:<timestamp>:<body>". Signature header is
// `v0=<hex>`. Timestamp arrives in X-Slack-Request-Timestamp.
func (s *slackVerifier) Verify(r *http.Request) error {
	sig := r.Header.Get(s.cfg.Header)
	ts := r.Header.Get("X-Slack-Request-Timestamp")
	if sig == "" || ts == "" {
		return fmt.Errorf("missing slack headers")
	}
	if !strings.HasPrefix(sig, "v0=") {
		return fmt.Errorf("malformed slack signature")
	}
	tsInt, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("bad timestamp")
	}
	if age := s.cfg.NowFn().Sub(time.Unix(tsInt, 0)); age > s.cfg.Tolerance || -age > s.cfg.Tolerance {
		return fmt.Errorf("timestamp outside tolerance")
	}
	body, err := readAndRestore(r)
	if err != nil {
		return err
	}
	mac := hmac.New(sha256.New, []byte(s.cfg.Secret))
	fmt.Fprintf(mac, "v0:%s:", ts)
	mac.Write(body)
	want := "v0=" + hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(sig), []byte(want)) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}

// ── Generic ───────────────────────────────────────────────────────────────

type genericVerifier struct{ cfg Config }

// Header value form: "<prefix><hex>" with prefix optional (e.g. "sha256=").
// Body is signed as-is with the configured algorithm.
func (g *genericVerifier) Verify(r *http.Request) error {
	raw := r.Header.Get(g.cfg.Header)
	if raw == "" {
		return fmt.Errorf("missing %s header", g.cfg.Header)
	}
	val := strings.TrimPrefix(raw, g.cfg.HeaderPrefix)
	body, err := readAndRestore(r)
	if err != nil {
		return err
	}
	mac, err := newHMAC(g.cfg.Algorithm, g.cfg.Secret)
	if err != nil {
		return err
	}
	mac.Write(body)
	want := hex.EncodeToString(mac.Sum(nil))
	if !hexEqual(val, want) {
		return fmt.Errorf("signature mismatch")
	}
	return nil
}
