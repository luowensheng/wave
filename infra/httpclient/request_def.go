package httpclient

// RequestDef is a named, reusable outbound HTTP request definition.
// Compatible with the "requests" project file format.
type RequestDef struct {
	File               string            `yaml:"file,omitempty"`
	URL                string            `yaml:"url,omitempty"`
	Method             string            `yaml:"method,omitempty"`          // default: GET
	Headers            map[string]string `yaml:"headers,omitempty"`
	Body               string            `yaml:"body,omitempty"`
	Timeout            int               `yaml:"timeout,omitempty"`         // seconds, default 30
	RetryCount         int               `yaml:"retry_count,omitempty"`
	RetryDelay         int               `yaml:"retry_delay,omitempty"`     // seconds, default 1
	Auth               *RequestAuth      `yaml:"auth,omitempty"`
	InsecureSkipVerify bool              `yaml:"insecure_skip_verify,omitempty"`
	FollowRedirects    *bool             `yaml:"follow_redirects,omitempty"` // default true
}

// RequestAuth holds HTTP Basic Auth credentials.
type RequestAuth struct {
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}
