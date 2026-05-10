// Package cookies builds HTTP cookies with the same-site / secure /
// domain conventions wave uses for auth cookies. It has no
// awareness of the auth feature; callers pass primitive policy fields.
package cookies

import "net/http"

// Policy is the subset of fields callers configure per-cookie.
type Policy struct {
	// SecureOverride forces the Secure flag on or off. nil means "auto"
	// (derived from the request).
	SecureOverride *string // unused placeholder kept for future bool override
	Secure         *bool
	SameSiteRaw    string // "None" | "Lax" | "Strict" | ""
	Domain         string
}

// Build returns a cookie with HttpOnly + path "/" set, with Secure and
// SameSite computed from the request and policy.
func Build(name, value string, policy Policy, r *http.Request, maxAge int) *http.Cookie {
	isSecure := IsSecureRequest(r)
	if policy.Secure != nil {
		isSecure = *policy.Secure
	}

	sameSite := SameSitePolicy(policy.SameSiteRaw, isSecure)
	if sameSite == http.SameSiteNoneMode {
		isSecure = true
	}

	c := &http.Cookie{
		Name:     name,
		Value:    value,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure,
		SameSite: sameSite,
		MaxAge:   maxAge,
	}
	if policy.Domain != "" {
		c.Domain = policy.Domain
	}
	return c
}

// IsSecureRequest reports whether the request was served over HTTPS,
// taking X-Forwarded-Proto into account for proxied deployments.
func IsSecureRequest(r *http.Request) bool {
	if proto := r.Header.Get("X-Forwarded-Proto"); proto == "https" {
		return true
	}
	if r.TLS != nil {
		return true
	}
	if r.URL != nil && r.URL.Scheme == "https" {
		return true
	}
	return false
}

// SameSitePolicy maps a raw config string to net/http's SameSite enum.
func SameSitePolicy(configValue string, isSecure bool) http.SameSite {
	switch configValue {
	case "None":
		return http.SameSiteNoneMode
	case "Lax", "":
		return http.SameSiteLaxMode
	case "Strict":
		return http.SameSiteStrictMode
	default:
		if isSecure {
			return http.SameSiteLaxMode
		}
		return http.SameSiteDefaultMode
	}
}
