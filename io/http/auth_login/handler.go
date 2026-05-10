package auth_login

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"
	texttemplate "text/template"

	"wave/infra/render"
)

// LoginResult carries the outcome of an authentication attempt.
type LoginResult struct {
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
}

// LoginFn is a function that performs login and returns a result.
type LoginFn func(username, password, authConfigName string) *LoginResult

// Config holds the configuration for the auth login handler.
// It mirrors usecases/auth_login.Config.
type Config struct {
	For               string
	RedirectOnSuccess string
	RedirectOnFailure string

	UsernameField        string
	PasswordField        string
	ConfirmPasswordField string
	EmailField           string

	ErrorTemplate     string
	ErrorTemplateStr  string
	ErrorRedirect     string
	ErrorResponseType string

	CookieSecure   *bool
	CookieSameSite string
}

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

func NewHandler(config *Config, login LoginFn) (http.HandlerFunc, error) {
	var errorTemplate *texttemplate.Template
	var templateErr error

	if config.ErrorTemplate != "" {
		errorTemplate, templateErr = texttemplate.ParseFiles(config.ErrorTemplate)
		if templateErr != nil {
			log.Printf("[WARN] Failed to parse error template file %s: %v", config.ErrorTemplate, templateErr)
		}
	} else if config.ErrorTemplateStr != "" {
		errorTemplate, templateErr = texttemplate.New("error").Parse(config.ErrorTemplateStr)
		if templateErr != nil {
			log.Printf("[WARN] Failed to parse error template string: %v", templateErr)
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		usernameField := valueOrDefault(config.UsernameField, "username")
		passwordField := valueOrDefault(config.PasswordField, "password")

		username := r.Form.Get(usernameField)
		password := r.Form.Get(passwordField)

		response := login(username, password, config.For)

		if !response.Success {
			log.Printf("[LOGIN ERROR]: %s (code: %s)", response.Error, response.Code)
			handleError(config, w, r, response, username, errorTemplate)
			return
		}

		log.Printf("[LOGIN SUCCESS]: %s", response.Message)

		switch response.Location {
		case "cookie":
			setCookie(config, w, r, response)
			redirectOnSuccess(config, w, r, response)

		case "header":
			w.Header().Set(response.Name, response.Value)
			sendJSON(w, response)

		default:
			http.Error(w, "Unexpected error: invalid token location", http.StatusInternalServerError)
		}
	}, nil
}

func handleError(config *Config, w http.ResponseWriter, r *http.Request, response *LoginResult, username string, errorTemplate *texttemplate.Template) {
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

	responseType := config.ErrorResponseType
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
			renderBasicErrorHTML(w, ctx)
		}

	case "redirect":
		fallthrough
	default:
		redirectURL := config.ErrorRedirect
		if redirectURL == "" {
			buffer, err := render.Render(config.RedirectOnFailure, ctx)
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

func setCookie(config *Config, w http.ResponseWriter, r *http.Request, response *LoginResult) {
	secure := isSecureRequest(r)
	if config.CookieSecure != nil {
		secure = *config.CookieSecure
	}

	sameSite := parseSameSite(config.CookieSameSite, secure)

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
}

func redirectOnSuccess(config *Config, w http.ResponseWriter, r *http.Request, response *LoginResult) {
	redirectURL := config.RedirectOnSuccess
	if redirectURL == "" && response.RedirectTo != "" {
		redirectURL = response.RedirectTo
	}
	if redirectURL == "" {
		redirectURL = "/"
	}

	log.Printf("[LOGIN] Redirecting to: %s", redirectURL)
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func sendJSON(w http.ResponseWriter, response *LoginResult) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func renderBasicErrorHTML(w http.ResponseWriter, ctx ErrorContext) {
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
