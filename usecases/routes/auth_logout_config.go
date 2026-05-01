// auth_logout_config.go
package routes

import (
	"easyserver/orchestrator/features/auth"
	"encoding/json"
	"html/template"
	"net/http"

	"log"
)

type AuthLogoutConfig struct {
	For               string `yaml:"for,omitempty"`
	RedirectOnSuccess string `yaml:"redirect_on_success,omitempty"`
	RedirectOnFailure string `yaml:"redirect_on_failure,omitempty"`

	// Error handling configuration
	ErrorTemplate     string `yaml:"error_template,omitempty"`      // Path to error template file
	ErrorTemplateStr  string `yaml:"error_template_str,omitempty"`  // Inline error template string
	ErrorRedirect     string `yaml:"error_redirect,omitempty"`      // Override redirect_on_failure
	ErrorResponseType string `yaml:"error_response_type,omitempty"` // "json", "html", "redirect" (default: auto-detect)

	// Cookie configuration overrides (optional)
	CookieSecure   *bool  `yaml:"cookie_secure,omitempty"`
	CookieSameSite string `yaml:"cookie_same_site,omitempty"` // "Strict", "Lax", "None"
}

type LogoutErrorContext struct {
	Success bool          `json:"success"`
	Error   string        `json:"error"`
	Code    string        `json:"code"`
	Message string        `json:"message"`
	Request *http.Request `json:"-"`
}

// CreateRoute implements servers.RouteConfig.
func (c *AuthLogoutConfig) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {

	// Pre-compile error template if provided
	var errorTemplate *template.Template
	var templateErr error

	if c.ErrorTemplate != "" {
		errorTemplate, templateErr = template.ParseFiles(c.ErrorTemplate)
		if templateErr != nil {
			log.Printf("[WARN] Failed to parse error template file %s: %v", c.ErrorTemplate, templateErr)
		}
	} else if c.ErrorTemplateStr != "" {
		errorTemplate, templateErr = template.New("error").Parse(c.ErrorTemplateStr)
		if templateErr != nil {
			log.Printf("[WARN] Failed to parse error template string: %v", templateErr)
		}
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// Perform logout using auth manager
		response := auth.Logout(r, c.For)

		if !response.Success {
			log.Printf("[LOGOUT ERROR]: %s (code: %s)", response.Error, response.Code)
			c.handleError(w, r, response, errorTemplate)
			return
		}

		log.Printf("[LOGOUT SUCCESS]: %s", response.Message)

		// Handle successful logout
		switch response.Location {
		case "cookie":
			c.clearCookie(w, r, response)
			c.redirectOnSuccess(w, r, response)

		case "header":
			w.Header().Set(response.Name, "")
			c.sendJSON(w, response)

		default:
			http.Error(w, "Unexpected error: invalid token location", http.StatusInternalServerError)
		}
	}, nil
}

func (c *AuthLogoutConfig) handleError(w http.ResponseWriter, r *http.Request, response *auth.LogoutResponse, errorTemplate *template.Template) {
	// Build error context
	ctx := LogoutErrorContext{
		Success: false,
		Error:   response.Error,
		Code:    response.Code,
		Message: response.Message,
		Request: r,
	}

	// Determine response type
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
			redirectURL = c.RedirectOnFailure
		}
		if redirectURL == "" {
			redirectURL = r.Referer()
		}
		if redirectURL == "" {
			redirectURL = "/"
		}

		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
	}
}

func (c *AuthLogoutConfig) clearCookie(w http.ResponseWriter, r *http.Request, response *auth.LogoutResponse) {
	// Determine cookie security settings
	secure := isSecureRequest(r)
	if c.CookieSecure != nil {
		secure = *c.CookieSecure
	}

	sameSite := parseSameSite(c.CookieSameSite, secure)

	cookie := &http.Cookie{
		Name:     response.Name,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: sameSite,
		MaxAge:   -1, // Delete cookie
	}

	http.SetCookie(w, cookie)
}

func (c *AuthLogoutConfig) redirectOnSuccess(w http.ResponseWriter, r *http.Request, response *auth.LogoutResponse) {
	redirectURL := c.RedirectOnSuccess
	if redirectURL == "" && response.RedirectTo != "" {
		redirectURL = response.RedirectTo
	}
	if redirectURL == "" {
		redirectURL = "/"
	}

	log.Printf("[LOGOUT] Redirecting to: %s", redirectURL)
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (c *AuthLogoutConfig) sendJSON(w http.ResponseWriter, response *auth.LogoutResponse) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

func (c *AuthLogoutConfig) renderBasicErrorHTML(w http.ResponseWriter, ctx LogoutErrorContext) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusUnauthorized)

	html := `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Logout Error</title>
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
        <div class="error-title">Logout Failed</div>
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
