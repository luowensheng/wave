// Tests focus on the SSRF guardrails — every place an attacker could
// trick the proxy into hitting an internal address or a non-allowed
// host. Outbound proxying itself uses net/http/httputil.ReverseProxy,
// which is well-covered upstream; we don't re-test it here.
package dynamic_forward

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func hit(t *testing.T, cfg *Config, r *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	h, err := cfg.CreateRoute("GET", "/proxy", nil)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}
	rr := httptest.NewRecorder()
	h(rr, r)
	return rr
}

func TestDF_Boot_InvalidTimeoutFails(t *testing.T) {
	_, err := (&Config{Timeout: "not-a-duration"}).CreateRoute("GET", "/proxy", nil)
	if err == nil {
		t.Fatal("expected boot error on invalid timeout")
	}
}

func TestDF_Boot_ValidTimeoutSucceeds(t *testing.T) {
	if _, err := (&Config{Timeout: "5s"}).CreateRoute("GET", "/proxy", nil); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDF_Defaults(t *testing.T) {
	cfg := &Config{}
	_, err := cfg.CreateRoute("GET", "/proxy", nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.URLSource != "query" {
		t.Fatalf("default URLSource should be 'query', got %q", cfg.URLSource)
	}
	if cfg.URLKey != "url" {
		t.Fatalf("default URLKey should be 'url', got %q", cfg.URLKey)
	}
}

func TestDF_MissingTargetURL400(t *testing.T) {
	rr := hit(t, &Config{}, httptest.NewRequest("GET", "/proxy", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "missing required query parameter") {
		t.Fatalf("body=%q", rr.Body.String())
	}
}

func TestDF_NonHTTPSchemeRejected(t *testing.T) {
	for _, scheme := range []string{"ftp", "file", "gopher", "javascript"} {
		rr := hit(t, &Config{}, httptest.NewRequest("GET", "/proxy?url="+scheme+"://example.com/", nil))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("scheme=%q: got %d", scheme, rr.Code)
		}
	}
}

func TestDF_AllowedDomainsEnforced(t *testing.T) {
	cfg := &Config{AllowedDomains: []string{"api.example.com", " Other.Example.com "}}
	// Allowed domain
	rr := hit(t, cfg, httptest.NewRequest("GET", "/proxy?url=https://api.example.com/x", nil))
	if rr.Code == http.StatusForbidden {
		t.Fatalf("api.example.com should pass allowlist, got 403")
	}
	// Case-insensitive whitespace-trimmed match
	rr = hit(t, cfg, httptest.NewRequest("GET", "/proxy?url=https://OTHER.example.com/x", nil))
	if rr.Code == http.StatusForbidden {
		t.Fatalf("other.example.com should pass allowlist (case-insensitive), got 403")
	}
	// Disallowed
	rr = hit(t, cfg, httptest.NewRequest("GET", "/proxy?url=https://evil.example.com/", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("disallowed domain should 403, got %d", rr.Code)
	}
}

func TestDF_BlockPrivateIPs_LoopbackLiterals(t *testing.T) {
	cfg := &Config{BlockPrivateIPs: true}
	for _, target := range []string{
		"http://127.0.0.1/",
		"http://127.0.0.1:8080/",
		"http://[::1]/",
		"http://0.0.0.0/",
	} {
		rr := hit(t, cfg, httptest.NewRequest("GET", "/proxy?url="+target, nil))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("target=%s: got %d, want 403", target, rr.Code)
		}
	}
}

func TestDF_BlockPrivateIPs_RFC1918(t *testing.T) {
	cfg := &Config{BlockPrivateIPs: true}
	for _, target := range []string{
		"http://10.0.0.1/",
		"http://10.255.255.1/",
		"http://172.16.0.1/",
		"http://172.31.255.1/",
		"http://192.168.1.1/",
		"http://192.168.0.100/",
	} {
		rr := hit(t, cfg, httptest.NewRequest("GET", "/proxy?url="+target, nil))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("RFC1918 %s: got %d, want 403", target, rr.Code)
		}
	}
}

func TestDF_BlockPrivateIPs_LinkLocalAndMulticast(t *testing.T) {
	cfg := &Config{BlockPrivateIPs: true}
	for _, target := range []string{
		"http://169.254.169.254/", // AWS metadata service — classic SSRF target
		"http://224.0.0.1/",       // multicast
	} {
		rr := hit(t, cfg, httptest.NewRequest("GET", "/proxy?url="+target, nil))
		if rr.Code != http.StatusForbidden {
			t.Fatalf("target=%s: got %d, want 403 — possible SSRF gap", target, rr.Code)
		}
	}
}

func TestDF_BlockPrivateIPs_AllowsPublicLiteral(t *testing.T) {
	// 1.1.1.1 is public; SSRF guard should not block it.
	cfg := &Config{BlockPrivateIPs: true}
	rr := hit(t, cfg, httptest.NewRequest("GET", "/proxy?url=http://1.1.1.1/", nil))
	if rr.Code == http.StatusForbidden {
		t.Fatalf("public IP should not be blocked by SSRF guard, got 403")
	}
}

func TestDF_URLSource_Header(t *testing.T) {
	cfg := &Config{URLSource: "header", URLKey: "X-Target"}
	// Missing header
	rr := hit(t, cfg, httptest.NewRequest("GET", "/proxy", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing header: got %d", rr.Code)
	}
	// Wrong scheme via header still rejected
	req := httptest.NewRequest("GET", "/proxy", nil)
	req.Header.Set("X-Target", "ftp://example.com/")
	rr = hit(t, cfg, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("ftp via header: got %d", rr.Code)
	}
}

func TestDF_UnsupportedURLSource(t *testing.T) {
	cfg := &Config{URLSource: "body"} // not supported
	rr := hit(t, cfg, httptest.NewRequest("GET", "/proxy?url=https://example.com/", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400 for unsupported url_source", rr.Code)
	}
}

func TestDF_MalformedURL400(t *testing.T) {
	rr := hit(t, &Config{}, httptest.NewRequest("GET", "/proxy?url=://malformed", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}

func TestDF_AllowedDomainsCombinedWithSSRFGuard(t *testing.T) {
	// Even if a hostname is in the allowlist, the SSRF check still
	// fires when BlockPrivateIPs is true. Useful when someone
	// allowlists "*.local" then sets the SSRF guard separately —
	// belt-and-suspenders.
	cfg := &Config{
		AllowedDomains:  []string{"localhost"},
		BlockPrivateIPs: true,
	}
	rr := hit(t, cfg, httptest.NewRequest("GET", "/proxy?url=http://localhost/", nil))
	if rr.Code != http.StatusForbidden {
		t.Fatalf("SSRF guard should override allowlist for localhost, got %d", rr.Code)
	}
}
