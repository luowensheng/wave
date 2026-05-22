package auth_signup

import (
	"encoding/json"
	htmltemplate "html/template"
	"log"
	"net/http"
	"strings"

	"github.com/luowensheng/wave/infra/render"
)

// SignupResult carries the outcome of a signup or login attempt.
type SignupResult struct {
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

// LoginFn is a function that performs login (used for auto-login after signup).
type LoginFn func(username, password, authConfigName string) *SignupResult

// SignupFn is a function that performs signup and returns a result.
type SignupFn func(username, password, passwordRepeat, authConfigName string) *SignupResult

// Config holds the configuration for the auth signup handler.
// It mirrors usecases/auth_signup.Config.
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

	AutoLogin bool

	CookieSecure   *bool
	CookieSameSite string
}

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

func NewHandler(config *Config, signup SignupFn, login LoginFn) (http.HandlerFunc, error) {
	var errorTemplate *htmltemplate.Template
	var templateErr error

	if config.ErrorTemplate != "" {
		errorTemplate, templateErr = htmltemplate.ParseFiles(config.ErrorTemplate)
		if templateErr != nil {
			log.Printf("[WARN] Failed to parse error template file %s: %v", config.ErrorTemplate, templateErr)
		}
	} else if config.ErrorTemplateStr != "" {
		errorTemplate, templateErr = htmltemplate.New("error").Parse(config.ErrorTemplateStr)
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
		confirmPasswordField := valueOrDefault(config.ConfirmPasswordField, "confirm_password")
		emailField := valueOrDefault(config.EmailField, "email")

		username := r.Form.Get(usernameField)
		password := r.Form.Get(passwordField)
		confirmPassword := r.Form.Get(confirmPasswordField)
		email := r.Form.Get(emailField)
		_ = email

		response := signup(username, password, confirmPassword, config.For)

		if !response.Success {
			log.Printf("[SIGNUP ERROR]: %s (code: %s)", response.Error, response.Code)
			handleError(config, w, r, response, username, email, errorTemplate)
			return
		}

		log.Printf("[SIGNUP SUCCESS]: User created: %s", username)

		if config.AutoLogin && login != nil {
			autoLoginAfterSignup(config, w, r, username, password, login)
		} else {
			handleSuccess(config, w, r, response)
		}
	}, nil
}

func handleError(config *Config, w http.ResponseWriter, r *http.Request, response *SignupResult, username, email string, errorTemplate *htmltemplate.Template) {
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
			redirectURL = "/signup"
		}

		http.Redirect(w, r, redirectURL, http.StatusSeeOther)
	}
}

func autoLoginAfterSignup(config *Config, w http.ResponseWriter, r *http.Request, username, password string, login LoginFn) {
	loginResponse := login(username, password, config.For)

	if !loginResponse.Success {
		log.Printf("[SIGNUP] Auto-login failed: %s", loginResponse.Error)
		redirectToSuccess(config, w, r, loginResponse)
		return
	}

	log.Printf("[SIGNUP] Auto-login successful for: %s", username)

	if loginResponse.Location == "cookie" {
		secure := isSecureRequest(r)
		if config.CookieSecure != nil {
			secure = *config.CookieSecure
		}

		sameSite := parseSameSite(config.CookieSameSite, secure)

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

	redirectToSuccess(config, w, r, loginResponse)
}

func handleSuccess(config *Config, w http.ResponseWriter, r *http.Request, response *SignupResult) {
	if isBrowserRequest(r) {
		redirectToSuccess(config, w, r, response)
	} else {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		json.NewEncoder(w).Encode(response)
	}
}

func redirectToSuccess(config *Config, w http.ResponseWriter, r *http.Request, response *SignupResult) {
	redirectURL := config.RedirectOnSuccess
	if redirectURL == "" && response.RedirectTo != "" {
		redirectURL = response.RedirectTo
	}
	if redirectURL == "" {
		redirectURL = "/"
	}

	log.Printf("[SIGNUP] Redirecting to: %s", redirectURL)
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

func renderBasicErrorHTML(w http.ResponseWriter, ctx SignupErrorContext) {
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
