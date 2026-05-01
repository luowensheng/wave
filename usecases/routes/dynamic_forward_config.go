package routes

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"
	"time"
)

// DynamicForwardConfig forwards requests to a URL provided dynamically in the request
type DynamicForwardConfig struct {
	// Source of the target URL: "query", "header", or "path"
	URLSource string `yaml:"url_source,omitempty" json:"url_source,omitempty"`
	// Name of the query param, header, or path segment to extract URL from
	URLKey string `yaml:"url_key,omitempty" json:"url_key,omitempty"`
	// Optional: allowlist of allowed domains (empty = allow all, use with caution)
	AllowedDomains []string `yaml:"allowed_domains,omitempty" json:"allowed_domains,omitempty"`
	// Block private/internal IPs to prevent SSRF (recommended: true)
	BlockPrivateIPs bool `yaml:"block_private_ips,omitempty" json:"block_private_ips,omitempty"`
	// IncludeHeaders to add to forwarded requests
	IncludeHeaders [][2]string `yaml:"include_headers,omitempty" json:"include_headers,omitempty"`
	// AllowInsecureRequests allows HTTPS with invalid certs
	AllowInsecureRequests bool `yaml:"allow_insecure_requests,omitempty" json:"allow_insecure_requests,omitempty"`
	// Timeout for the forwarded request
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	// StripPrefix removes a prefix from the request path before forwarding
	StripPrefix string `yaml:"strip_prefix,omitempty" json:"strip_prefix,omitempty"`
}

// CreateRoute implements dynamic forwarding with SSRF protection
func (c *DynamicForwardConfig) CreateRoute(method, path string, data map[string]string) (http.HandlerFunc, error) {
	if c.URLSource == "" {
		c.URLSource = "query"
	}
	if c.URLKey == "" {
		c.URLKey = "url"
	}

	// Parse optional timeout
	var timeout time.Duration
	if c.Timeout != "" {
		var err error
		timeout, err = time.ParseDuration(c.Timeout)
		if err != nil {
			return nil, fmt.Errorf("invalid timeout duration: %w", err)
		}
	}

	// Build allowed domains map for fast lookup
	allowedMap := make(map[string]bool, len(c.AllowedDomains))
	for _, d := range c.AllowedDomains {
		allowedMap[strings.ToLower(strings.TrimSpace(d))] = true
	}

	return func(w http.ResponseWriter, r *http.Request) {
		// 1. Extract target URL from request
		targetStr, err := c.extractTargetURL(r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		// 2. Parse and validate target URL
		targetURL, err := url.Parse(targetStr)
		if err != nil {
			log.Printf("Invalid target URL '%s': %v", targetStr, err)
			http.Error(w, "Invalid target URL", http.StatusBadRequest)
			return
		}

		// 3. Security: validate scheme
		if targetURL.Scheme != "http" && targetURL.Scheme != "https" {
			http.Error(w, "Only http/https schemes allowed", http.StatusBadRequest)
			return
		}

		// 4. Security: domain allowlist check
		if len(allowedMap) > 0 {
			host := strings.ToLower(targetURL.Hostname())
			if !allowedMap[host] {
				log.Printf("Blocked request to disallowed domain: %s", host)
				http.Error(w, "Domain not allowed", http.StatusForbidden)
				return
			}
		}

		// 5. Security: block private/internal IPs if enabled
		if c.BlockPrivateIPs {
			host := targetURL.Hostname()
			if c.isPrivateIP(host) {
				log.Printf("Blocked request to private IP: %s", host)
				http.Error(w, "Access to internal addresses denied", http.StatusForbidden)
				return
			}
		}

		// 6. Build the reverse proxy
		proxy := &httputil.ReverseProxy{
			Director: func(req *http.Request) {
				// Preserve original path after stripping prefix
				reqPath := req.URL.Path
				if c.StripPrefix != "" {
					reqPath = strings.TrimPrefix(reqPath, c.StripPrefix)
				}

				// Join with target URL path
				finalPath, _ := url.JoinPath(targetURL.Path, reqPath)

				req.URL.Scheme = targetURL.Scheme
				req.URL.Host = targetURL.Host
				req.URL.Path = finalPath
				// Preserve original query params, or merge if needed
				// req.URL.RawQuery is kept as-is; customize if you want to forward source params

				req.Host = targetURL.Host

				// Add custom headers
				for _, item := range c.IncludeHeaders {
					if len(item) >= 2 {
						req.Header.Set(item[0], item[1])
					}
				}

				log.Printf("Dynamic forward: %s %s -> %s://%s%s", req.Method, req.URL.Path, targetURL.Scheme, targetURL.Host, req.URL.Path)
			},
			Transport: &http.Transport{
				TLSClientConfig:    &tls.Config{InsecureSkipVerify: c.AllowInsecureRequests},
				MaxIdleConns:       100,
				IdleConnTimeout:    90 * time.Second,
				ForceAttemptHTTP2:  true,
				DisableCompression: true,
			},
			ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
				if r.Context().Err() != nil {
					return // Client disconnected
				}
				log.Printf("Dynamic proxy error: %v", err)
				http.Error(w, "Bad Gateway", http.StatusBadGateway)
			},
			FlushInterval: -1, // Enable streaming/SSE/WebSocket passthrough
		}

		// Apply timeout if configured
		if timeout > 0 {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			r = r.WithContext(ctx)
		}

		proxy.ServeHTTP(w, r)
	}, nil
}

// extractTargetURL gets the target URL from query param, header, or path
func (c *DynamicForwardConfig) extractTargetURL(r *http.Request) (string, error) {
	path := r.URL.Path

	switch c.URLSource {
	case "query":
		target := r.URL.Query().Get(c.URLKey)
		if target == "" {
			return "", fmt.Errorf("missing required query parameter: %s", c.URLKey)
		}
		return target, nil

	case "header":
		target := r.Header.Get(c.URLKey)
		if target == "" {
			return "", fmt.Errorf("missing required header: %s", c.URLKey)
		}
		return target, nil

	case "path":
		// Example: /proxy/https://example.com/api -> extract after /proxy/
		prefix := strings.TrimSuffix(path, "/*") // if path is like "/proxy/*"
		if prefix == "" {
			prefix = path
		}
		target := strings.TrimPrefix(r.URL.Path, prefix)
		target = strings.TrimPrefix(target, "/")
		if target == "" {
			return "", fmt.Errorf("missing target URL in path")
		}
		return target, nil

	default:
		return "", fmt.Errorf("unsupported url_source: %s", c.URLSource)
	}
}

// isPrivateIP checks if a hostname resolves to a private/internal IP range
func (c *DynamicForwardConfig) isPrivateIP(hostname string) bool {
	// Skip if it's clearly a domain (but still resolve to be safe)
	ips, err := net.LookupIP(hostname)
	if err != nil {
		// If lookup fails, check if it looks like a private IP literal
		return c.isPrivateIPLiteral(hostname)
	}
	for _, ip := range ips {
		if c.isPrivateIPLiteral(ip.String()) {
			return true
		}
	}
	return false
}

// isPrivateIPLiteral checks common private/reserved IP ranges
func (c *DynamicForwardConfig) isPrivateIPLiteral(ipStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return false
	}
	// Private, loopback, link-local, multicast, or unspecified
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() ||
		ipInCIDR(ip, "10.0.0.0/8") || ipInCIDR(ip, "172.16.0.0/12") ||
		ipInCIDR(ip, "192.168.0.0/16") || ipInCIDR(ip, "127.0.0.0/8") ||
		ipInCIDR(ip, "169.254.0.0/16") || ipInCIDR(ip, "224.0.0.0/4")
}

func ipInCIDR(ip net.IP, cidr string) bool {
	_, net, err := net.ParseCIDR(cidr)
	if err != nil {
		return false
	}
	return net.Contains(ip)
}
