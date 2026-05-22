package httpclient

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// LoadFromFile loads a RequestDef from a YAML file.
func LoadFromFile(path string) (*RequestDef, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("load request def %q: %w", path, err)
	}
	var def RequestDef
	if err := yaml.Unmarshal(data, &def); err != nil {
		return nil, fmt.Errorf("parse request def %q: %w", path, err)
	}
	return &def, nil
}

// Merge creates a new RequestDef with base values overridden by non-zero fields from override.
func Merge(base, override *RequestDef) *RequestDef {
	merged := *base
	if override.URL != "" {
		merged.URL = override.URL
	}
	if override.Method != "" {
		merged.Method = override.Method
	}
	if override.Body != "" {
		merged.Body = override.Body
	}
	if override.Timeout != 0 {
		merged.Timeout = override.Timeout
	}
	if override.RetryCount != 0 {
		merged.RetryCount = override.RetryCount
	}
	if override.RetryDelay != 0 {
		merged.RetryDelay = override.RetryDelay
	}
	if override.Auth != nil {
		merged.Auth = override.Auth
	}
	if override.InsecureSkipVerify {
		merged.InsecureSkipVerify = true
	}
	if override.FollowRedirects != nil {
		merged.FollowRedirects = override.FollowRedirects
	}
	// Merge headers: override wins per key.
	if len(override.Headers) > 0 {
		merged.Headers = make(map[string]string, len(base.Headers)+len(override.Headers))
		for k, v := range base.Headers {
			merged.Headers[k] = v
		}
		for k, v := range override.Headers {
			merged.Headers[k] = v
		}
	}
	return &merged
}
