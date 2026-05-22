package auth_login

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	texttemplate "text/template"

	"github.com/luowensheng/wave/infra/render"
)

type Config struct {
	For               string `yaml:"for,omitempty"`
	RedirectOnSuccess string `yaml:"redirect_on_success,omitempty"`
	RedirectOnFailure string `yaml:"redirect_on_failure,omitempty"`

	UsernameField        string `yaml:"username_field,omitempty"`
	PasswordField        string `yaml:"password_field,omitempty"`
	ConfirmPasswordField string `yaml:"confirm_password_field,omitempty"`
	EmailField           string `yaml:"email_field,omitempty"`

	ErrorTemplate     string `yaml:"error_template,omitempty"`
	ErrorTemplateStr  string `yaml:"error_template_str,omitempty"`
	ErrorRedirect     string `yaml:"error_redirect,omitempty"`
	ErrorResponseType string `yaml:"error_response_type,omitempty"`

	CookieSecure   *bool  `yaml:"cookie_secure,omitempty"`
	CookieSameSite string `yaml:"cookie_same_site,omitempty"`
}

// LoginResponse is the result of a login attempt.
type LoginResponse struct {
	Success       bool
	Location      string
	Error         string
	Code          string
	Message       string
	Details       map[string]string
	Name          string
	Value         string
	TokenDuration int
	RedirectTo    string
	UserID        int
	Username      string
	// ExtraCookies are written verbatim *in addition* to the
	// orchestrator-issued auth cookie. Used by plugin-backed auth
	// flows that need to install their own state (e.g. SAML
	// RelayState).
	ExtraCookies []*http.Cookie
}

// LoginFn is a function that performs login.
// Inject this before using Config.CreateRoute.
var LoginFn func(username, password, authConfigName string) *LoginResponse

// LoginFnWithRequest is the request-aware variant. When set, the
// handler prefers it over LoginFn so plugin-backed auth can see the
// inbound headers and cookies. May be nil; LoginFn is the fallback.
var LoginFnWithRequest func(username, password, authConfigName string, r *http.Request) *LoginResponse

// ErrorContext is the data passed to error templates.
type ErrorContext struct {
	Success  bool              `json:"success"`
	Error    string            `json:"error"`
	Code     string            `json:"code"`
	Message  string            `json:"message"`
	Details  map[string]string `json:"details"`
	Username string            `json:"username,omitempty"`
	Email    string            `json:"email,omitempty"`
	FormData map[string]string `json:"form_data,omitempty"`
	Request  *http.Request     `json:"-"`
}

// CreateRoute implements servers.RouteConfig.
func (c *Config) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {
	var errorTemplate *texttemplate.Template
	var templateErr error

	if c.ErrorTemplate != "" {
		errorTemplate, templateErr = texttemplate.ParseFiles(c.ErrorTemplate)
		if templateErr != nil {
			log.Printf("[WARN] Failed to parse error template file %s: %v", c.ErrorTemplate, templateErr)
		}
	} else if c.ErrorTemplateStr != "" {
		errorTemplate, templateErr = texttemplate.New("error").Parse(c.ErrorTemplateStr)
		if templateErr != nil {
			log.Printf("[WARN] Failed to parse error template string: %v", templateErr)
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		usernameField := valueOrDefault(c.UsernameField, "username")
		passwordField := valueOrDefault(c.PasswordField, "password")

		username := r.Form.Get(usernameField)
		password := r.Form.Get(passwordField)

		var response *LoginResponse
		switch {
		case LoginFnWithRequest != nil:
			response = LoginFnWithRequest(username, password, c.For, r)
		case LoginFn != nil:
			response = LoginFn(username, password, c.For)
		default:
			response = &LoginResponse{
				Success: false,
				Error:   "login not configured",
				Code:    "not_configured",
			}
		}

		if !response.Success {
			log.Printf("[LOGIN ERROR]: %s (code: %s)", response.Error, response.Code)
			c.handleError(w, r, response, username, errorTemplate)
			return
		}

		log.Printf("[LOGIN SUCCESS]: %s", response.Message)

		switch response.Location {
		case "cookie":
			c.setCookie(w, r, response)
			c.redirectOnSuccess(w, r, response)

		case "header":
			w.Header().Set(response.Name, response.Value)
			c.sendJSON(w, response)

		default:
			http.Error(w, "Unexpected error: invalid token location", http.StatusInternalServerError)
		}
	}, nil
}

func (c *Config) handleError(w http.ResponseWriter, r *http.Request, response *LoginResponse, username string, errorTemplate *texttemplate.Template) {
	ctx := ErrorContext{
		Success:  false,
		Error:    response.Error,
		Code:     response.Code,
		Message:  response.Message,
		Details:  response.Details,
		Username: username,
		FormData: map[string]string{
			"username": username,
		},
		Request: r,
	}

	responseType := c.ErrorResponseType
	if responseType == "" {
		if isBrowserRequest(r) {
			responseType = "redirect"
		} else {
			responseType = "json"
		}
	}

	switch responseType {
	case "json":
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(ctx)

	case "html":
		if errorTemplate != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusUnauthorized)
			if err := errorTemplate.Execute(w, ctx); err != nil {
				log.Printf("[ERROR] Failed to execute error template: %v", err)
				http.Error(w, "Internal server error", http.StatusInternalServerError)
			}
		} else {
			c.renderBasicErrorHTML(w, ctx)
		}

	case "redirect":
		fallthrough
	default:
		redirectURL := c.ErrorRedirect
		if redirectURL == "" {
			buffer, err := render.Render(c.RedirectOnFailure, ctx)
			if err == nil {
				redirectURL = buffer.String()
			}
		}
		if redirectURL == "" {
			redirectURL = r.Referer()
		}
		if redirectURL == "" {
			redirectURL = "/login"
		}

		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
	}
}

func (c *Config) setCookie(w http.ResponseWriter, r *http.Request, response *LoginResponse) {
	secure := isSecureRequest(r)
	if c.CookieSecure != nil {
		secure = *c.CookieSecure
	}

	sameSite := parseSameSite(c.CookieSameSite, secure)

	cookie := &http.Cookie{
		Name:     response.Name,
		Value:    response.Value,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		MaxAge:   response.TokenDuration,
	}

	http.SetCookie(w, cookie)
	for _, extra := range response.ExtraCookies {
		if extra != nil {
			http.SetCookie(w, extra)
		}
	}
}

func (c *Config) redirectOnSuccess(w http.ResponseWriter, r *http.Request, response *LoginResponse) {
	redirectURL := c.RedirectOnSuccess
	if redirectURL == "" && response.RedirectTo != "" {
		redirectURL = response.RedirectTo
	}
	if redirectURL == "" {
		redirectURL = "/"
	}

	log.Printf("[LOGIN] Redirecting to: %s", redirectURL)
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (c *Config) sendJSON(w http.ResponseWriter, response *LoginResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (c *Config) renderBasicErrorHTML(w http.ResponseWriter, ctx ErrorContext) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)

	html := `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Login Error</title>
    <style>
        body { font-family: sans-serif; max-width: 600px; margin: 50px auto; padding: 20px; }
        .error { background: #fee; border: 1px solid #fcc; border-radius: 4px; padding: 15px; margin: 20px 0; }
        .error-title { color: #c33; font-weight: bold; margin-bottom: 10px; }
        .error-message { color: #666; }
        .error-code { color: #999; font-size: 0.9em; margin-top: 10px; }
        .back-link { margin-top: 20px; }
        a { color: #0066cc; text-decoration: none; }
        a:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="error">
        <div class="error-title">Login Failed</div>
        <div class="error-message">` + htmlEscape(ctx.Error) + `</div>
        <div class="error-code">Error Code: ` + htmlEscape(ctx.Code) + `</div>
    </div>
    <div class="back-link">
        <a href="javascript:history.back()">&#8592; Go Back</a>
    </div>
</body>
</html>`

	w.Write([]byte(html))
}

func htmlEscape(s string) string {
	return strings.NewReplacer(
		"&", "&amp;",
		"<", "&lt;",
		">", "&gt;",
		`"`, "&#34;",
		"'", "&#39;",
	).Replace(s)
}

func valueOrDefault(value, defaultValue string) string {
	if value != "" {
		return value
	}
	return defaultValue
}

func isBrowserRequest(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	userAgent := r.Header.Get("User-Agent")
	return strings.Contains(accept, "text/html") || strings.Contains(userAgent, "Mozilla")
}

func isSecureRequest(r *http.Request) bool {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		return true
	}
	if r.TLS != nil {
		return true
	}
	if r.URL.Scheme == "https" {
		return true
	}
	return false
}

func parseSameSite(value string, secure bool) http.SameSite {
	switch value {
	case "None":
		return http.SameSiteNoneMode
	case "Lax", "":
		return http.SameSiteLaxMode
	case "Strict":
		return http.SameSiteStrictMode
	default:
		if secure {
			return http.SameSiteLaxMode
		}
		return http.SameSiteDefaultMode
	}
}
