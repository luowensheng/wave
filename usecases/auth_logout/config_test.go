package auth_logout

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// stubLogoutFn installs a LogoutFn that returns `resp` and records
// the args it was called with. Returns a restore func.
func stubLogoutFn(t *testing.T, resp *LogoutResponse) (called *int, restore func()) {
	t.Helper()
	prev := LogoutFn
	n := 0
	LogoutFn = func(r *http.Request, name string) *LogoutResponse {
		n++
		return resp
	}
	return &n, func() { LogoutFn = prev }
}

func hit(t *testing.T, cfg *Config, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	h, err := cfg.CreateRoute("POST", "/logout", nil)
	if err != nil {
		t.Fatalf("CreateRoute: %v", err)
	}
	rr := httptest.NewRecorder()
	h(rr, req)
	return rr
}

func TestLogout_NotConfigured_FallsToError(t *testing.T) {
	prev := LogoutFn
	LogoutFn = nil
	defer func() { LogoutFn = prev }()

	req := httptest.NewRequest("POST", "/logout", nil)
	req.Header.Set("Accept", "application/json")
	rr := hit(t, &Config{}, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d, want 401", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "not_configured") {
		t.Fatalf("expected not_configured code in body, got %q", rr.Body.String())
	}
}

func TestLogout_CookieLocationClearsCookie(t *testing.T) {
	_, restore := stubLogoutFn(t, &LogoutResponse{
		Success: true, Location: "cookie", Name: "session",
	})
	defer restore()

	rr := hit(t, &Config{RedirectOnSuccess: "/bye"}, httptest.NewRequest("POST", "/logout", nil))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("got %d, want 303", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/bye" {
		t.Fatalf("Location=%q want /bye", loc)
	}

	// Inspect Set-Cookie for clearing semantics.
	setCookie := rr.Header().Get("Set-Cookie")
	if !strings.Contains(setCookie, "session=") {
		t.Fatalf("Set-Cookie should target session, got %q", setCookie)
	}
	if !strings.Contains(setCookie, "Max-Age=0") {
		t.Fatalf("Set-Cookie should expire (Max-Age=0), got %q", setCookie)
	}
	if !strings.Contains(setCookie, "HttpOnly") {
		t.Fatalf("Set-Cookie should be HttpOnly, got %q", setCookie)
	}
}

func TestLogout_HeaderLocationSetsHeader(t *testing.T) {
	_, restore := stubLogoutFn(t, &LogoutResponse{
		Success: true, Location: "header", Name: "Authorization",
	})
	defer restore()

	rr := hit(t, &Config{}, httptest.NewRequest("POST", "/logout", nil))

	if got := rr.Header().Get("Authorization"); got != "" {
		t.Fatalf("Authorization header should be cleared (empty), got %q", got)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type should be application/json, got %q", ct)
	}
}

func TestLogout_ErrorRedirectFallbackChain(t *testing.T) {
	_, restore := stubLogoutFn(t, &LogoutResponse{Success: false, Error: "bad token"})
	defer restore()

	// ErrorRedirect wins
	req := httptest.NewRequest("POST", "/logout", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (browser)")
	rr := hit(t, &Config{ErrorRedirect: "/login?err=1"}, req)
	if rr.Header().Get("Location") != "/login?err=1" {
		t.Fatalf("ErrorRedirect should win, got %q", rr.Header().Get("Location"))
	}

	// RedirectOnFailure used when ErrorRedirect empty
	req = httptest.NewRequest("POST", "/logout", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	rr = hit(t, &Config{RedirectOnFailure: "/oops"}, req)
	if rr.Header().Get("Location") != "/oops" {
		t.Fatalf("RedirectOnFailure should win, got %q", rr.Header().Get("Location"))
	}

	// Referer used when both empty
	req = httptest.NewRequest("POST", "/logout", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	req.Header.Set("Referer", "https://app.example.com/dashboard")
	rr = hit(t, &Config{}, req)
	if rr.Header().Get("Location") != "https://app.example.com/dashboard" {
		t.Fatalf("Referer should win, got %q", rr.Header().Get("Location"))
	}

	// Fall back to /
	req = httptest.NewRequest("POST", "/logout", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	rr = hit(t, &Config{}, req)
	if rr.Header().Get("Location") != "/" {
		t.Fatalf("/ should be final fallback, got %q", rr.Header().Get("Location"))
	}
}

func TestLogout_ErrorResponseTypeJSON(t *testing.T) {
	_, restore := stubLogoutFn(t, &LogoutResponse{Success: false, Error: "bad", Code: "expired"})
	defer restore()

	rr := hit(t, &Config{ErrorResponseType: "json"}, httptest.NewRequest("POST", "/logout", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"error":"bad"`) {
		t.Fatalf("body missing error: %q", rr.Body.String())
	}
}

func TestLogout_CookieSecureOverride(t *testing.T) {
	_, restore := stubLogoutFn(t, &LogoutResponse{Success: true, Location: "cookie", Name: "sid"})
	defer restore()

	secure := true
	rr := hit(t, &Config{CookieSecure: &secure}, httptest.NewRequest("POST", "/logout", nil))
	if !strings.Contains(rr.Header().Get("Set-Cookie"), "Secure") {
		t.Fatalf("expected Secure flag when CookieSecure=true, got %q", rr.Header().Get("Set-Cookie"))
	}
}

func TestLogout_RedirectOnSuccessRespectsResponseRedirectTo(t *testing.T) {
	_, restore := stubLogoutFn(t, &LogoutResponse{
		Success: true, Location: "cookie", Name: "sid", RedirectTo: "/from-response",
	})
	defer restore()

	// RedirectOnSuccess wins when set
	rr := hit(t, &Config{RedirectOnSuccess: "/from-config"}, httptest.NewRequest("POST", "/logout", nil))
	if rr.Header().Get("Location") != "/from-config" {
		t.Fatalf("RedirectOnSuccess should win, got %q", rr.Header().Get("Location"))
	}

	// response.RedirectTo wins when config empty
	rr = hit(t, &Config{}, httptest.NewRequest("POST", "/logout", nil))
	if rr.Header().Get("Location") != "/from-response" {
		t.Fatalf("response.RedirectTo should win, got %q", rr.Header().Get("Location"))
	}
}

func TestLogout_BrowserDetectionPicksRedirectByDefault(t *testing.T) {
	_, restore := stubLogoutFn(t, &LogoutResponse{Success: false, Error: "boom"})
	defer restore()

	// Browser → redirect (302/303)
	req := httptest.NewRequest("POST", "/logout", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0")
	rr := hit(t, &Config{}, req)
	if rr.Code != http.StatusSeeOther {
		t.Fatalf("browser request should redirect, got %d", rr.Code)
	}

	// API client → JSON
	req = httptest.NewRequest("POST", "/logout", nil)
	req.Header.Set("Accept", "application/json")
	rr = hit(t, &Config{}, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("api request should 401 with JSON, got %d", rr.Code)
	}
}
