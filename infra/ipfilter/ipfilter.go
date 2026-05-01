package ipfilter

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// FilterMode defines the filtering strategy
type FilterMode int

const (
	// WhitelistMode allows only IPs in the whitelist
	WhitelistMode FilterMode = iota
	// BlacklistMode blocks IPs in the blacklist
	BlacklistMode
	// CombinedMode checks whitelist first, then blacklist
	CombinedMode
)

// IPFilter holds the configuration for IP filtering
type IPFilter struct {
	whitelist       []*net.IPNet
	blacklist       []*net.IPNet
	whitelistPrefix []string // IP prefixes for whitelist (e.g., "192.168")
	blacklistPrefix []string // IP prefixes for blacklist (e.g., "10.0")
	mode            FilterMode
	trustedProxy    []string // For X-Forwarded-For header parsing
}

// NewIPFilter creates a new IP filter instance
func NewIPFilter(mode FilterMode) *IPFilter {
	return &IPFilter{
		whitelist:       make([]*net.IPNet, 0),
		blacklist:       make([]*net.IPNet, 0),
		whitelistPrefix: make([]string, 0),
		blacklistPrefix: make([]string, 0),
		mode:            mode,
		trustedProxy:    make([]string, 0),
	}
}

func NewIPFilterCombined(whitelist, blacklist []string) *IPFilter {
	var mode FilterMode

	switch {
	case len(whitelist) > 0 && len(blacklist) > 0:
		mode = CombinedMode
	case len(whitelist) > 0:
		mode = WhitelistMode
	case len(blacklist) > 0:
		mode = BlacklistMode
	default:
		return nil
	}

	f := NewIPFilter(mode)

	if len(whitelist) > 0 {
		_ = f.AddToWhitelist(whitelist...)
	}
	if len(blacklist) > 0 {
		_ = f.AddToBlacklist(blacklist...)
	}

	return f
}

// isValidIPOrCIDR checks if a string is a valid IP address or CIDR notation
func isValidIPOrCIDR(s string) bool {
	// Try parsing as IP first
	if net.ParseIP(s) != nil {
		return true
	}
	// Try parsing as CIDR
	_, _, err := net.ParseCIDR(s)
	return err == nil
}

// AddToWhitelist adds IP addresses, CIDR ranges, or prefixes to the whitelist
// Automatically detects the format and adds to appropriate list
func (f *IPFilter) AddToWhitelist(entries ...string) error {
	for _, entry := range entries {
		if isValidIPOrCIDR(entry) {
			// It's a valid IP or CIDR, add to exact list
			ipNet, err := parseIPOrCIDR(entry)
			if err != nil {
				return fmt.Errorf("invalid whitelist IP/CIDR %s: %w", entry, err)
			}
			f.whitelist = append(f.whitelist, ipNet)
		} else {
			// Treat as prefix
			f.whitelistPrefix = append(f.whitelistPrefix, entry)
		}
	}
	return nil
}

// AddToBlacklist adds IP addresses, CIDR ranges, or prefixes to the blacklist
// Automatically detects the format and adds to appropriate list
func (f *IPFilter) AddToBlacklist(entries ...string) error {
	for _, entry := range entries {
		if isValidIPOrCIDR(entry) {
			// It's a valid IP or CIDR, add to exact list
			ipNet, err := parseIPOrCIDR(entry)
			if err != nil {
				return fmt.Errorf("invalid blacklist IP/CIDR %s: %w", entry, err)
			}
			f.blacklist = append(f.blacklist, ipNet)
		} else {
			// Treat as prefix
			f.blacklistPrefix = append(f.blacklistPrefix, entry)
		}
	}
	return nil
}

// AddToWhitelistPrefix adds IP prefixes to the whitelist (explicit method for clarity)
func (f *IPFilter) AddToWhitelistPrefix(prefixes ...string) {
	f.whitelistPrefix = append(f.whitelistPrefix, prefixes...)
}

// AddToBlacklistPrefix adds IP prefixes to the blacklist (explicit method for clarity)
func (f *IPFilter) AddToBlacklistPrefix(prefixes ...string) {
	f.blacklistPrefix = append(f.blacklistPrefix, prefixes...)
}

// SetTrustedProxies sets the list of trusted proxy IPs for X-Forwarded-For parsing
func (f *IPFilter) SetTrustedProxies(proxies ...string) {
	f.trustedProxy = make([]string, len(proxies))
	copy(f.trustedProxy, proxies)
}

// parseIPOrCIDR parses an IP address or CIDR notation
func parseIPOrCIDR(ipStr string) (*net.IPNet, error) {
	// If it doesn't contain '/', treat as single IP
	if !strings.Contains(ipStr, "/") {
		ip := net.ParseIP(ipStr)
		if ip == nil {
			return nil, fmt.Errorf("invalid IP address: %s", ipStr)
		}
		// Convert to /32 for IPv4 or /128 for IPv6
		if ip.To4() != nil {
			ipStr += "/32"
		} else {
			ipStr += "/128"
		}
	}

	_, ipNet, err := net.ParseCIDR(ipStr)
	return ipNet, err
}

// GetClientIP extracts the real client IP from the request
func (f *IPFilter) GetClientIP(r *http.Request) string {
	// Check X-Forwarded-For header if trusted proxies are configured
	if len(f.trustedProxy) > 0 {
		xff := r.Header.Get("X-Forwarded-For")
		if xff != "" {
			// Get the first IP in the chain
			ips := strings.Split(xff, ",")
			if len(ips) > 0 {
				clientIP := strings.TrimSpace(ips[0])
				return normalizeIP(clientIP)
			}
		}
	}

	// Check X-Real-IP header
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return normalizeIP(realIP)
	}

	// Fall back to RemoteAddr
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return normalizeIP(r.RemoteAddr)
	}

	return normalizeIP(host)
}

// normalizeIP converts IPv6 localhost (::1) to IPv4 localhost (127.0.0.1)
// and handles other IP normalization needs
func normalizeIP(ip string) string {
	ip = strings.TrimSpace(ip)

	// Parse the IP to validate and normalize it
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return ip // Return as-is if it's not a valid IP
	}

	// Convert IPv6 localhost to IPv4 localhost for consistency
	if parsedIP.IsLoopback() {
		if parsedIP.To4() == nil {
			// It's IPv6 loopback (::1), convert to IPv4
			return "127.0.0.1"
		}
	}

	// Return the string representation of the parsed IP
	// This handles IPv4-mapped IPv6 addresses (e.g., ::ffff:192.0.2.1 -> 192.0.2.1)
	if ip4 := parsedIP.To4(); ip4 != nil {
		return ip4.String()
	}

	return parsedIP.String()
}



// IsWhitelisted checks if an IP is in the whitelist (exact match or prefix match)
func (f *IPFilter) IsWhitelisted(ip string) bool {
	// Check exact IP/CIDR matches first
	parsedIP := net.ParseIP(ip)
	if parsedIP != nil {
		for _, ipNet := range f.whitelist {
			if ipNet.Contains(parsedIP) {
				return true
			}
		}
	}

	// Check prefix matches
	for _, prefix := range f.whitelistPrefix {
		if strings.HasPrefix(ip, prefix) {
			return true
		}
	}

	return false
}

// IsBlacklisted checks if an IP is in the blacklist (exact match or prefix match)
func (f *IPFilter) IsBlacklisted(ip string) bool {
	// Check exact IP/CIDR matches first
	parsedIP := net.ParseIP(ip)
	if parsedIP != nil {
		for _, ipNet := range f.blacklist {
			if ipNet.Contains(parsedIP) {
				return true
			}
		}
	}

	// Check prefix matches
	for _, prefix := range f.blacklistPrefix {
		if strings.HasPrefix(ip, prefix) {
			return true
		}
	}

	return false
}

// IsAllowed determines if an IP should be allowed based on the filter mode
func (f *IPFilter) IsAllowed(ip string) bool {
	switch f.mode {
	case WhitelistMode:
		return f.IsWhitelisted(ip)
	case BlacklistMode:
		return !f.IsBlacklisted(ip)
	case CombinedMode:
		// If IP is whitelisted, allow it regardless of blacklist
		if f.IsWhitelisted(ip) {
			return true
		}
		// If not whitelisted, check if it's blacklisted
		return !f.IsBlacklisted(ip)
	default:
		return false
	}
}

// Middleware returns an HTTP middleware that filters requests based on IP
func (f *IPFilter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clientIP := f.GetClientIP(r)
		fmt.Println("GOT NEW IP: ", clientIP)

		if !f.IsAllowed(clientIP) {
			http.Error(w, "Access forbidden", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// MiddlewareFunc returns a middleware function that can wrap http.HandlerFunc
func (f *IPFilter) MiddlewareFunc(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {

		clientIP := f.GetClientIP(r)
		fmt.Println("GOT NEW IP: ", clientIP, " -> f.IsAllowed(clientIP):", f.IsAllowed(clientIP))

		if !f.IsAllowed(clientIP) {
			http.Error(w, "Access forbidden", http.StatusForbidden)
			return
		}

		next(w, r)
	}
}

// CheckRequest is a simple function to check a specific request
func (f *IPFilter) CheckRequest(r *http.Request) (allowed bool, clientIP string) {
	clientIP = f.GetClientIP(r)
	allowed = f.IsAllowed(clientIP)
	return
}

// Example usage and helper functions

// NewWhitelistFilter creates a preconfigured whitelist filter
func NewWhitelistFilter(allowedIPs ...string) (*IPFilter, error) {
	filter := NewIPFilter(WhitelistMode)
	if err := filter.AddToWhitelist(allowedIPs...); err != nil {
		return nil, err
	}
	return filter, nil
}

// NewWhitelistFilterWithPrefixes creates a whitelist filter with both IPs and prefixes
func NewWhitelistFilterWithPrefixes(allowedIPs []string, allowedPrefixes []string) (*IPFilter, error) {
	filter := NewIPFilter(WhitelistMode)
	if len(allowedIPs) > 0 {
		if err := filter.AddToWhitelist(allowedIPs...); err != nil {
			return nil, err
		}
	}
	if len(allowedPrefixes) > 0 {
		filter.AddToWhitelistPrefix(allowedPrefixes...)
	}
	return filter, nil
}

// NewBlacklistFilter creates a preconfigured blacklist filter
func NewBlacklistFilter(blockedIPs ...string) (*IPFilter, error) {
	filter := NewIPFilter(BlacklistMode)
	if err := filter.AddToBlacklist(blockedIPs...); err != nil {
		return nil, err
	}
	return filter, nil
}

// NewBlacklistFilterWithPrefixes creates a blacklist filter with both IPs and prefixes
func NewBlacklistFilterWithPrefixes(blockedIPs []string, blockedPrefixes []string) (*IPFilter, error) {
	filter := NewIPFilter(BlacklistMode)
	if len(blockedIPs) > 0 {
		if err := filter.AddToBlacklist(blockedIPs...); err != nil {
			return nil, err
		}
	}
	if len(blockedPrefixes) > 0 {
		filter.AddToBlacklistPrefix(blockedPrefixes...)
	}
	return filter, nil
}


// Example usage:
/*
func main() {
	// Create a whitelist filter with both exact IPs and prefixes
	filter, err := NewWhitelistFilterWithPrefixes(
		[]string{"127.0.0.1", "192.168.1.0/24"}, // Exact IPs/CIDRs
		[]string{"10.0", "172.16"},               // Prefixes
	)
	if err != nil {
		log.Fatal(err)
	}

	// Or add prefixes to existing filter
	filter.AddToWhitelistPrefix("203.0.113") // Allow all IPs starting with 203.0.113

	// Set trusted proxies if behind a load balancer
	filter.SetTrustedProxies("10.0.0.100", "10.0.0.101")

	// Create your handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Hello, authorized user!"))
	})

	// Apply the IP filter middleware
	http.Handle("/", filter.Middleware(handler))

	// Start server
	log.Println("Server starting on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

// Blacklist example with prefixes
func blacklistExample() {
	filter, err := NewBlacklistFilterWithPrefixes(
		[]string{"1.2.3.4"},        // Block specific IP
		[]string{"192.168", "10"},  // Block all IPs starting with 192.168 or 10
	)
	if err != nil {
		log.Fatal(err)
	}

	// Test some IPs
	fmt.Println("192.168.1.1 allowed:", filter.IsAllowed("192.168.1.1")) // false
	fmt.Println("10.0.0.1 allowed:", filter.IsAllowed("10.0.0.1"))       // false
	fmt.Println("203.0.113.1 allowed:", filter.IsAllowed("203.0.113.1")) // true
}

// Combined mode example
func combinedExample() {
	filter := NewIPFilter(CombinedMode)

	// Whitelist company networks (mix of exact IPs, CIDRs, and prefixes)
	filter.AddToWhitelist(
		"127.0.0.1",        // Exact IP
		"10.0.0.0/8",       // CIDR range
		"203.0.113",        // Prefix (auto-detected)
		"198.51.100",       // Another prefix
	)

	// Blacklist known bad actors (mix of formats)
	filter.AddToBlacklist(
		"203.0.113.100",    // Specific bad IP (exact)
		"198.51.100.200",   // Another specific IP (exact)
		"192.168",          // Block entire prefix (auto-detected)
		"172.16.0.0/12",    // Block CIDR range
	)

	// In combined mode:
	// - Whitelist takes precedence over blacklist
	// - If not in whitelist, then check blacklist
}

// Alternative usage with manual checking:
func handleRequest(w http.ResponseWriter, r *http.Request) {
	filter := getIPFilter() // Your filter instance

	allowed, clientIP := filter.CheckRequest(r)
	if !allowed {
		log.Printf("Blocked request from IP: %s", clientIP)
		http.Error(w, "Access denied", http.StatusForbidden)
		return
	}

	// Process allowed request
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Access granted"))
}

// Auto-detection examples:
// filter.AddToBlacklist("203.0.113.100") -> Added as exact IP
// filter.AddToBlacklist("192.168")       -> Added as prefix (not valid IP/CIDR)
// filter.AddToBlacklist("10.0.0.0/8")    -> Added as CIDR range
// filter.AddToBlacklist("192.168.")      -> Added as prefix (not valid IP/CIDR)

// You can still use explicit methods if you want to be clear:
// filter.AddToBlacklistPrefix("192.168")  -> Explicitly added as prefix
// filter.AddToBlacklist("192.168.1.1")    -> Auto-detected as exact IP
*/
// // GetClientIP extracts the real client IP from the request
// func (f *IPFilter) GetClientIP(r *http.Request) string {
// 	// Check X-Forwarded-For header if trusted proxies are configured
// 	if len(f.trustedProxy) > 0 {
// 		xff := r.Header.Get("X-Forwarded-For")
// 		if xff != "" {
// 			// Get the first IP in the chain
// 			ips := strings.Split(xff, ",")
// 			if len(ips) > 0 {
// 				return strings.TrimSpace(ips[0])
// 			}
// 		}
// 	}

// 	// Check X-Real-IP header
// 	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
// 		return realIP
// 	}

// 	// Fall back to RemoteAddr
// 	host, _, err := net.SplitHostPort(r.RemoteAddr)
// 	if err != nil {
// 		return r.RemoteAddr
// 	}

// 	// ::1
// 	return host
// }