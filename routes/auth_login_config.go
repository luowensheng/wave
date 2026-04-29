// auth_login_config.go
package routes

import (
	"easyserver/auth"
	"easyserver/render"
	"encoding/json"
	"net/http"
	"strings"
	"text/template"

	"log"
)

type AuthLoginConfig struct {
	For               string `yaml:"for,omitempty"`
	RedirectOnSuccess string `yaml:"redirect_on_success,omitempty"`
	RedirectOnFailure string `yaml:"redirect_on_failure,omitempty"`

	UsernameField        string `yaml:"username_field,omitempty"`
	PasswordField        string `yaml:"password_field,omitempty"`
	ConfirmPasswordField string `yaml:"confirm_password_field,omitempty"`
	EmailField           string `yaml:"email_field,omitempty"`

	// Error handling configuration
	ErrorTemplate     string `yaml:"error_template,omitempty"`      // Path to error template file
	ErrorTemplateStr  string `yaml:"error_template_str,omitempty"`  // Inline error template string
	ErrorRedirect     string `yaml:"error_redirect,omitempty"`      // Override redirect_on_failure
	ErrorResponseType string `yaml:"error_response_type,omitempty"` // "json", "html", "redirect" (default: auto-detect)

	// Cookie configuration overrides (optional)
	CookieSecure   *bool  `yaml:"cookie_secure,omitempty"`
	CookieSameSite string `yaml:"cookie_same_site,omitempty"` // "Strict", "Lax", "None"
}

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
func (c *AuthLoginConfig) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {

	// Pre-compile error template if provided
	var errorTemplate *template.Template
	var templateErr error

	if c.ErrorTemplate != "" {
		// Load template from file
		errorTemplate, templateErr = template.ParseFiles(c.ErrorTemplate)
		if templateErr != nil {
			log.Printf("[WARN] Failed to parse error template file %s: %v", c.ErrorTemplate, templateErr)
		}
	} else if c.ErrorTemplateStr != "" {
		// Parse inline template string
		errorTemplate, templateErr = template.New("error").Parse(c.ErrorTemplateStr)
		if templateErr != nil {
			log.Printf("[WARN] Failed to parse error template string: %v", templateErr)
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Parse form data
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		// Get form fields
		usernameField := valueOrDefault(c.UsernameField, "username")
		passwordField := valueOrDefault(c.PasswordField, "password")

		username := r.Form.Get(usernameField)
		password := r.Form.Get(passwordField)

		// Create login request
		loginReq := auth.LoginForm{
			Username: username,
			Password: password,
		}

		// Perform login using auth manager
		response := auth.Login(loginReq, c.For)

		if !response.Success {
			log.Printf("[LOGIN ERROR]: %s (code: %s)", response.Error, response.Code)
			c.handleError(w, r, response, username, errorTemplate)
			return
		}

		log.Printf("[LOGIN SUCCESS]: %s", response.Message)

		// Handle successful login
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

func (c *AuthLoginConfig) handleError(w http.ResponseWriter, r *http.Request, response *auth.LoginResponse, username string, errorTemplate *template.Template) {
	// Build error context
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

	// Determine response type
	responseType := c.ErrorResponseType
	if responseType == "" {
		// Auto-detect based on request
		if isBrowserRequest(r) {
			responseType = "redirect" // Default for browsers
		} else {
			responseType = "json" // Default for API clients
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
			// Fallback to basic HTML
			c.renderBasicErrorHTML(w, ctx)
		}

	case "redirect":
		fallthrough
	default:
		// Redirect with error context
		redirectURL := c.ErrorRedirect
		if redirectURL == "" {
			buffer, err := render.Render(c.RedirectOnFailure, ctx, template.FuncMap{
				"getUser": func() *auth.PublicUser {
					value := r.Context().Value(auth.UserContextKey)
					user, ok := value.(*auth.PublicUser)
					// common.PrintJSON(common.Object{
					// 	"auth_user": user,
					// })
					if !ok {
						panic("No user")
					}
					return user
				},
			})
			if err == nil {
				redirectURL = buffer.String()
			}

		}
		if redirectURL == "" {
			redirectURL = r.Referer() // Fallback to referer
		}
		if redirectURL == "" {
			redirectURL = "/login" // Last resort fallback
		}

		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
	}
}

func (c *AuthLoginConfig) setCookie(w http.ResponseWriter, r *http.Request, response *auth.LoginResponse) {
	// Determine cookie security settings
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
}

func (c *AuthLoginConfig) redirectOnSuccess(w http.ResponseWriter, r *http.Request, response *auth.LoginResponse) {
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

func (c *AuthLoginConfig) sendJSON(w http.ResponseWriter, response *auth.LoginResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (c *AuthLoginConfig) renderBasicErrorHTML(w http.ResponseWriter, ctx ErrorContext) {
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
        <div class="error-message">` + template.HTMLEscapeString(ctx.Error) + `</div>
        <div class="error-code">Error Code: ` + template.HTMLEscapeString(ctx.Code) + `</div>
    </div>
    <div class="back-link">
        <a href="javascript:history.back()">← Go Back</a>
    </div>
</body>
</html>`

	w.Write([]byte(html))
}

// Helper functions
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
