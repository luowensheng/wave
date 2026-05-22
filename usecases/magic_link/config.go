// Package magic_link implements two route types:
//
//	type: magic-link-request   POST /login/request {"email": "..."}
//	type: magic-link-consume   GET  /login/verify?token=...
//
// Together they implement passwordless email login. The request route
// accepts an email, asks the configured Issuer to mint a token, and
// emails the user a link of the form `<callback_url>?token=<token>`.
// The consume route validates the token and creates a session by
// invoking the same `auth-login` machinery — so anything downstream
// that already knows how to authenticate a user works unchanged.
//
// State lives in the verify token store (in-memory or SQLite); no
// extra schema beyond what infra/verify creates.
package magic_link

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/luowensheng/wave/infra/mailer"
	"github.com/luowensheng/wave/infra/verify"
)

// RequestConfig handles the "send me a magic link" POST.
type RequestConfig struct {
	// CallbackURL is the URL the email link points to (the consume
	// route). The token is appended as `?token=<value>`.
	CallbackURL string `yaml:"callback_url,omitempty" json:"callback_url,omitempty"`
	// FromEmail and Subject control the outgoing message. Body is a
	// text/template rendering of the EmailBody field with .Link in scope.
	FromEmail   string `yaml:"from_email,omitempty" json:"from_email,omitempty"`
	Subject     string `yaml:"subject,omitempty" json:"subject,omitempty"`
	// EmailBody is an inline plain-text template. Takes precedence over
	// EmailBodyFile when both are set. .Link, .Email and .MinutesValid
	// are in scope.
	EmailBody     string `yaml:"email_body,omitempty" json:"email_body,omitempty"`
	EmailBodyFile string `yaml:"email_body_file,omitempty" json:"email_body_file,omitempty"`
	// EmailBodyHTML / EmailBodyHTMLFile produce a text/html alternative
	// body (sent as multipart/alternative alongside the text body).
	// Optional — when unset, the email is plain-text only.
	EmailBodyHTML     string `yaml:"email_body_html,omitempty" json:"email_body_html,omitempty"`
	EmailBodyHTMLFile string `yaml:"email_body_html_file,omitempty" json:"email_body_html_file,omitempty"`
	TTLSeconds        int    `yaml:"ttl_seconds,omitempty" json:"ttl_seconds,omitempty"` // default 600 (10min)
}

// ConsumeConfig handles GET <callback>?token=...
type ConsumeConfig struct {
	// SuccessRedirect is the URL to redirect to after successful
	// consumption (e.g. /dashboard). Empty → returns 200 + JSON.
	SuccessRedirect string `yaml:"success_redirect,omitempty" json:"success_redirect,omitempty"`
}

// ── package-level state ───────────────────────────────────────────────────

const subject = "magic-link"

var (
	issuer    *verify.Issuer
	loginFn   = func(ctx context.Context, email string, w http.ResponseWriter, r *http.Request) error {
		return fmt.Errorf("magic-link: LoginFn not wired (orchestrator should call SetLoginFn)")
	}
)

// SetIssuer is called from the orchestrator at boot.
func SetIssuer(i *verify.Issuer) { issuer = i }

// SetLoginFn injects the function that creates a session for the given
// email after a token is consumed. The orchestrator wires this to the
// existing auth-login pipeline.
func SetLoginFn(fn func(ctx context.Context, email string, w http.ResponseWriter, r *http.Request) error) {
	loginFn = fn
}

// ── request route ─────────────────────────────────────────────────────────

func (c *RequestConfig) CreateRoute(method, path string, args map[string]string) (http.HandlerFunc, error) {
	if c.CallbackURL == "" {
		return nil, fmt.Errorf("magic-link request: callback_url required")
	}
	subj := c.Subject
	if subj == "" {
		subj = "Your sign-in link"
	}
	// Resolve body templates once at boot. Precedence: inline string
	// wins over file path; file is read once and cached in the closure.
	body, err := resolveBody(c.EmailBody, c.EmailBodyFile,
		"Click to sign in:\n\n{{.Link}}\n\nLink expires in {{.MinutesValid}} minutes.")
	if err != nil {
		return nil, fmt.Errorf("magic-link request email_body_file: %w", err)
	}
	htmlBody, err := resolveBody(c.EmailBodyHTML, c.EmailBodyHTMLFile, "")
	if err != nil {
		return nil, fmt.Errorf("magic-link request email_body_html_file: %w", err)
	}

	ttl := time.Duration(c.TTLSeconds) * time.Second
	if ttl <= 0 {
		ttl = 10 * time.Minute
	}
	from := c.FromEmail

	return func(w http.ResponseWriter, r *http.Request) {
		if issuer == nil {
			http.Error(w, "magic-link issuer not initialized", http.StatusInternalServerError)
			return
		}
		var req struct {
			Email string `json:"email"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Email == "" {
			http.Error(w, "email required", http.StatusBadRequest)
			return
		}
		tok, err := issuer.Issue(r.Context(), verify.IssueOpts{
			Subject: subject, Value: req.Email, TTL: ttl,
		})
		if err != nil {
			http.Error(w, "issue failed", http.StatusInternalServerError)
			return
		}
		link := c.CallbackURL
		if strings.Contains(link, "?") {
			link += "&token=" + string(tok)
		} else {
			link += "?token=" + string(tok)
		}
		data := map[string]any{
			"Link":         link,
			"Email":        req.Email,
			"MinutesValid": int(ttl.Minutes()),
		}
		text, err := mailer.RenderText(body, data)
		if err != nil {
			http.Error(w, "render failed", http.StatusInternalServerError)
			return
		}
		var html string
		if htmlBody != "" {
			html, err = mailer.RenderHTML(htmlBody, data)
			if err != nil {
				http.Error(w, "render failed", http.StatusInternalServerError)
				return
			}
		}
		if err := mailer.Send(mailer.Message{
			From: from, To: []string{req.Email},
			Subject: subj, TextBody: text, HTMLBody: html,
		}); err != nil {
			http.Error(w, "send failed", http.StatusInternalServerError)
			return
		}
		// Always return success so the endpoint isn't a user-existence
		// oracle (auth-bypass mitigation).
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]any{"sent": true})
	}, nil
}

// ── consume route ─────────────────────────────────────────────────────────

func (c *ConsumeConfig) CreateRoute(method, path string, args map[string]string) (http.HandlerFunc, error) {
	return func(w http.ResponseWriter, r *http.Request) {
		if issuer == nil {
			http.Error(w, "magic-link issuer not initialized", http.StatusInternalServerError)
			return
		}
		token := r.URL.Query().Get("token")
		if token == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}
		email, err := issuer.Consume(r.Context(), subject, verify.Token(token))
		if err != nil {
			http.Error(w, "invalid or expired token", http.StatusUnauthorized)
			return
		}
		if err := loginFn(r.Context(), email, w, r); err != nil {
			http.Error(w, "login failed: "+err.Error(), http.StatusInternalServerError)
			return
		}
		if c.SuccessRedirect != "" {
			http.Redirect(w, r, c.SuccessRedirect, http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"email": email, "ok": true})
	}, nil
}

// resolveBody picks an email-body template using inline-wins-over-file
// precedence, with the supplied default when neither is set. The file
// is read once at boot so subsequent requests pay no I/O.
func resolveBody(inline, path, defaultTpl string) (string, error) {
	if strings.TrimSpace(inline) != "" {
		return inline, nil
	}
	if path == "" {
		return defaultTpl, nil
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(b), nil
}
