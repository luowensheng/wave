package routes

type RouteOutput struct {
	StatusCode *int        `yaml:"status_code"`
	Headers    [][2]string `yaml:"headers"`
	Body       string      `yaml:"body"`
}

type Request struct {
	Type    string      `yaml:"type"`
	Method  string      `yaml:"method"`
	URL     string      `yaml:"url"`
	Headers [][2]string `yaml:"headers"`
	Body    string      `yaml:"body"`
}

type Response struct {
	Transform string            `yaml:"transform"`
	Stream    bool              `yaml:"stream"`
	Output    map[string]string `yaml:"output"`
}

// Method              string      `yaml:"method"`
// Dir                 string      `yaml:"dir,omitempty"`
// FilePath            string      `yaml:"filepath,omitempty"`
// ForwardURL          string      `yaml:"forward_url,omitempty"`
// Prettify            bool        `yaml:"prettify,omitempty"`
// Auth                []string    `yaml:"auth,omitempty"`
// IncludeHeaders      [][2]string `yaml:"include_headers,omitempty"`
// FileIgnorePatterns  []string    `yaml:"file_ignore_patterns,omitempty"`
// CatchAll            bool        `json:"catch_all"`
// Source              string      `yaml:"source"`
// Execute             string      `yaml:"execute"`
// ExecutePath         string      `yaml:"execute_path"`
// ReturnFile          bool        `yaml:"return_file"`
// OutputTemplate      string      `yaml:"output_template"`
// ExpectedContentType string      `yaml:"expected_content_type"`
// ResponseContentType string      `yaml:"response_content_type"`
// EnhancedMode        bool        `yaml:"emhanced_mode"`

// For               string `yaml:"for,omitempty"`
// RedirectOnSuccess string `yaml:"redirect_on_success,omitempty"`
// RedirectOnFailure string `yaml:"redirect_on_failure,omitempty"`

// UsernameField        string `yaml:"username_field,omitempty"`
// PasswordField        string `yaml:"password_field,omitempty"`
// ConfirmPasswordField string `yaml:"confirm_password_field,omitempty"`
// EmailField           string `yaml:"email_field,omitempty"`

// Request  *Request     `yaml:"request,omitempty"`
// Response *Response    `yaml:"response,omitempty"`
// Output   *RouteOutput `yaml:"output"`

// ValidateCSRF bool `yaml:"validate_csrf"`
// IncludeCSRF  bool `yaml:"include_csrf"`
