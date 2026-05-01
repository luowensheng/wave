// auth_signup_config.go
package routes

import (
	"easyserver/orchestrator/features/auth"
	"easyserver/infra/render"
	"encoding/json"
	"html/template"
	"net/http"

	"log"
)

type AuthSignupConfig struct {
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

	// Auto-login after successful signup
	AutoLogin bool `yaml:"auto_login,omitempty"` // Default: false

	// Cookie configuration overrides (optional, only used if auto_login is true)
	CookieSecure   *bool  `yaml:"cookie_secure,omitempty"`
	CookieSameSite string `yaml:"cookie_same_site,omitempty"` // "Strict", "Lax", "None"
}

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
func (c *AuthSignupConfig) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {

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
		// Parse form data
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Failed to parse form", http.StatusBadRequest)
			return
		}

		// Get form fields
		usernameField := valueOrDefault(c.UsernameField, "username")
		passwordField := valueOrDefault(c.PasswordField, "password")
		confirmPasswordField := valueOrDefault(c.ConfirmPasswordField, "confirm_password")
		emailField := valueOrDefault(c.EmailField, "email")

		username := r.Form.Get(usernameField)
		password := r.Form.Get(passwordField)
		confirmPassword := r.Form.Get(confirmPasswordField)
		email := r.Form.Get(emailField)

		// Create signup request
		signupForm := auth.SignupForm{
			Username:       username,
			Password:       password,
			PasswordRepeat: confirmPassword,
		}

		// Perform signup using auth manager
		response := auth.Signup(signupForm, c.For)

		if !response.Success {
			log.Printf("[SIGNUP ERROR]: %s (code: %s)", response.Error, response.Code)
			c.handleError(w, r, response, username, email, errorTemplate)
			return
		}

		log.Printf("[SIGNUP SUCCESS]: User created: %s", username)

		// Handle successful signup
		if c.AutoLogin {
			// Automatically log in the user after signup
			c.autoLoginAfterSignup(w, r, username, password)
		} else {
			// Just redirect or send response
			c.handleSuccess(w, r, response)
		}
	}, nil
}

func (c *AuthSignupConfig) handleError(w http.ResponseWriter, r *http.Request, response *auth.LoginResponse, username, email string, errorTemplate *template.Template) {
	// Build error context
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

func (c *AuthSignupConfig) autoLoginAfterSignup(w http.ResponseWriter, r *http.Request, username, password string) {
	// Perform login
	loginReq := auth.LoginForm{
		Username: username,
		Password: password,
	}

	loginResponse := auth.Login(loginReq, c.For)

	if !loginResponse.Success {
		log.Printf("[SIGNUP] Auto-login failed: %s", loginResponse.Error)
		// Still redirect to success page, but without being logged in
		c.redirectToSuccess(w, r, loginResponse)
		return
	}

	log.Printf("[SIGNUP] Auto-login successful for: %s", username)

	// Set cookie if token location is cookie
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

func (c *AuthSignupConfig) handleSuccess(w http.ResponseWriter, r *http.Request, response *auth.LoginResponse) {
	if isBrowserRequest(r) {
		c.redirectToSuccess(w, r, response)
	} else {
		// Return JSON for API clients
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(response)
	}
}

func (c *AuthSignupConfig) redirectToSuccess(w http.ResponseWriter, r *http.Request, response *auth.LoginResponse) {
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

func (c *AuthSignupConfig) renderBasicErrorHTML(w http.ResponseWriter, ctx SignupErrorContext) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusBadRequest)

	detailsHTML := ""
	if len(ctx.Details) > 0 {
		detailsHTML = "<div class=\"error-details\"><ul>"
		for field, issue := range ctx.Details {
			detailsHTML += "<li><strong>" + template.HTMLEscapeString(field) + ":</strong> " + template.HTMLEscapeString(issue) + "</li>"
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
        <div class="error-message">` + template.HTMLEscapeString(ctx.Error) + `</div>
        ` + detailsHTML + `
        <div class="error-code">Error Code: ` + template.HTMLEscapeString(ctx.Code) + `</div>
    </div>
    <div class="back-link">
        <a href="javascript:history.back()">← Go Back</a>
    </div>
</body>
</html>`

	w.Write([]byte(html))
}
