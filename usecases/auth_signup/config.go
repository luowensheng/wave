package auth_signup

import (
	"encoding/json"
	htmltemplate "html/template"
	"log"
	"net/http"
	"strings"

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

	AutoLogin bool `yaml:"auto_login,omitempty"`

	CookieSecure   *bool  `yaml:"cookie_secure,omitempty"`
	CookieSameSite string `yaml:"cookie_same_site,omitempty"`
}

// LoginResponse is used for both signup and auto-login results.
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
}

// SignupFn is a function that performs signup.
// Inject this before using Config.CreateRoute.
var SignupFn func(username, password, passwordRepeat, authConfigName string) *LoginResponse

// LoginFn is a function that performs login (used for auto-login after signup).
// Inject this before using Config.CreateRoute.
var LoginFn func(username, password, authConfigName string) *LoginResponse

// SignupErrorContext is the data passed to error templates.
type SignupErrorContext struct {
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
	var errorTemplate *htmltemplate.Template
	var templateErr error

	if c.ErrorTemplate != "" {
		errorTemplate, templateErr = htmltemplate.ParseFiles(c.ErrorTemplate)
		if templateErr != nil {
			log.Printf("[WARN] Failed to parse error template file %s: %v", c.ErrorTemplate, templateErr)
		}
	} else if c.ErrorTemplateStr != "" {
		errorTemplate, templateErr = htmltemplate.New("error").Parse(c.ErrorTemplateStr)
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
		confirmPasswordField := valueOrDefault(c.ConfirmPasswordField, "confirm_password")
		emailField := valueOrDefault(c.EmailField, "email")

		username := r.Form.Get(usernameField)
		password := r.Form.Get(passwordField)
		confirmPassword := r.Form.Get(confirmPasswordField)
		email := r.Form.Get(emailField)

		var response *LoginResponse
		if SignupFn != nil {
			response = SignupFn(username, password, confirmPassword, c.For)
		} else {
			response = &LoginResponse{
				Success: false,
				Error:   "signup not configured",
				Code:    "not_configured",
			}
		}

		if !response.Success {
			log.Printf("[SIGNUP ERROR]: %s (code: %s)", response.Error, response.Code)
			c.handleError(w, r, response, username, email, errorTemplate)
			return
		}

		log.Printf("[SIGNUP SUCCESS]: User created: %s", username)

		if c.AutoLogin && LoginFn != nil {
			c.autoLoginAfterSignup(w, r, username, password)
		} else {
			c.handleSuccess(w, r, response)
		}
	}, nil
}

func (c *Config) handleError(w http.ResponseWriter, r *http.Request, response *LoginResponse, username, email string, errorTemplate *htmltemplate.Template) {
	ctx := SignupErrorContext{
		Success:  false,
		Error:    response.Error,
		Code:     response.Code,
		Message:  response.Message,
		Details:  response.Details,
		Username: username,
		Email:    email,
		FormData: map[string]string{
			"username": username,
			"email":    email,
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
		w.WriteHeader(http.StatusBadRequest)
		json.NewEncoder(w).Encode(ctx)

	case "html":
		if errorTemplate != nil {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
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
			redirectURL = "/signup"
		}

		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
	}
}

func (c *Config) autoLoginAfterSignup(w http.ResponseWriter, r *http.Request, username, password string) {
	loginResponse := LoginFn(username, password, c.For)

	if !loginResponse.Success {
		log.Printf("[SIGNUP] Auto-login failed: %s", loginResponse.Error)
		c.redirectToSuccess(w, r, loginResponse)
		return
	}

	log.Printf("[SIGNUP] Auto-login successful for: %s", username)

	if loginResponse.Location == "cookie" {
		secure := isSecureRequest(r)
		if c.CookieSecure != nil {
			secure = *c.CookieSecure
		}

		sameSite := parseSameSite(c.CookieSameSite, secure)

		cookie := &http.Cookie{
			Name:     loginResponse.Name,
			Value:    loginResponse.Value,
			Path:     "/",
			HttpOnly: true,
			Secure:   secure,
			SameSite: sameSite,
			MaxAge:   loginResponse.TokenDuration,
		}

		http.SetCookie(w, cookie)
	}

	c.redirectToSuccess(w, r, loginResponse)
}

func (c *Config) handleSuccess(w http.ResponseWriter, r *http.Request, response *LoginResponse) {
	if isBrowserRequest(r) {
		c.redirectToSuccess(w, r, response)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(response)
	}
}

func (c *Config) redirectToSuccess(w http.ResponseWriter, r *http.Request, response *LoginResponse) {
	redirectURL := c.RedirectOnSuccess
	if redirectURL == "" && response.RedirectTo != "" {
		redirectURL = response.RedirectTo
	}
	if redirectURL == "" {
		redirectURL = "/"
	}

	log.Printf("[SIGNUP] Redirecting to: %s", redirectURL)
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func (c *Config) renderBasicErrorHTML(w http.ResponseWriter, ctx SignupErrorContext) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)

	detailsHTML := ""
	if len(ctx.Details) > 0 {
		detailsHTML = "<div class=\"error-details\"><ul>"
		for field, issue := range ctx.Details {
			detailsHTML += "<li><strong>" + htmltemplate.HTMLEscapeString(field) + ":</strong> " + htmltemplate.HTMLEscapeString(issue) + "</li>"
		}
		detailsHTML += "</ul></div>"
	}

	html := `<!DOCTYPE html>
<html>
<head>
    <meta charset="utf-8">
    <title>Signup Error</title>
    <style>
        body { font-family: sans-serif; max-width: 600px; margin: 50px auto; padding: 20px; }
        .error { background: #fee; border: 1px solid #fcc; border-radius: 4px; padding: 15px; margin: 20px 0; }
        .error-title { color: #c33; font-weight: bold; margin-bottom: 10px; }
        .error-message { color: #666; margin-bottom: 10px; }
        .error-details { color: #666; margin-top: 10px; }
        .error-details ul { margin: 5px 0; padding-left: 20px; }
        .error-code { color: #999; font-size: 0.9em; margin-top: 10px; }
        .back-link { margin-top: 20px; }
        a { color: #0066cc; text-decoration: none; }
        a:hover { text-decoration: underline; }
    </style>
</head>
<body>
    <div class="error">
        <div class="error-title">Signup Failed</div>
        <div class="error-message">` + htmltemplate.HTMLEscapeString(ctx.Error) + `</div>
        ` + detailsHTML + `
        <div class="error-code">Error Code: ` + htmltemplate.HTMLEscapeString(ctx.Code) + `</div>
    </div>
    <div class="back-link">
        <a href="javascript:history.back()">&#8592; Go Back</a>
    </div>
</body>
</html>`

	w.Write([]byte(html))
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
