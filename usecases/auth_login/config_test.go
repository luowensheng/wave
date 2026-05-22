package auth_login

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// stubLoginFn installs LoginFn returning `resp` and captures the
// username/password it was called with. Returns a restore func.
type loginCall struct{ username, password, name string }

func stubLoginFn(t *testing.T, resp *LoginResponse) (calls *[]loginCall, restore func()) {
	t.Helper()
	prevFn := LoginFn
	prevReq := LoginFnWithRequest
	c := []loginCall{}
	LoginFn = func(u, p, n string) *LoginResponse {
		c = append(c, loginCall{u, p, n})
		return resp
	}
	LoginFnWithRequest = nil
	return &c, func() { LoginFn = prevFn; LoginFnWithRequest = prevReq }
}

func formPost(body url.Values) *http.Request {
	r := httptest.NewRequest("POST", "/login", strings.NewReader(body.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestLogin_NotConfigured(t *testing.T) {
	prevFn := LoginFn
	prevReq := LoginFnWithRequest
	LoginFn = nil
	LoginFnWithRequest = nil
	defer func() { LoginFn = prevFn; LoginFnWithRequest = prevReq }()

	cfg := &Config{ErrorResponseType: "json"}
	h, err := cfg.CreateRoute("POST", "/login", nil)
	if err != nil {
		t.Fatal(err)
	}
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{"username": {"x"}, "password": {"y"}}))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "not_configured") {
		t.Fatalf("body=%q", rr.Body.String())
	}
}

func TestLogin_DefaultFieldNames(t *testing.T) {
	calls, restore := stubLoginFn(t, &LoginResponse{
		Success: true, Location: "cookie", Name: "sid", Value: "abc", TokenDuration: 3600,
	})
	defer restore()

	cfg := &Config{For: "primary"}
	h, _ := cfg.CreateRoute("POST", "/login", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{"username": {"ada"}, "password": {"secret"}}))

	if len(*calls) != 1 {
		t.Fatalf("expected 1 LoginFn call, got %d", len(*calls))
	}
	if c := (*calls)[0]; c.username != "ada" || c.password != "secret" || c.name != "primary" {
		t.Fatalf("got call %+v", c)
	}
}

func TestLogin_CustomFieldNames(t *testing.T) {
	calls, restore := stubLoginFn(t, &LoginResponse{Success: true, Location: "cookie", Name: "s"})
	defer restore()

	cfg := &Config{UsernameField: "email", PasswordField: "pw"}
	h, _ := cfg.CreateRoute("POST", "/login", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{"email": {"a@b.com"}, "pw": {"x"}}))

	if (*calls)[0].username != "a@b.com" || (*calls)[0].password != "x" {
		t.Fatalf("custom fields not honored: %+v", (*calls)[0])
	}
	_ = rr // not checking response — covered elsewhere
}

func TestLogin_SuccessSetsCookieAndRedirects(t *testing.T) {
	_, restore := stubLoginFn(t, &LoginResponse{
		Success: true, Location: "cookie", Name: "sid", Value: "tok-123",
		TokenDuration: 3600,
	})
	defer restore()

	cfg := &Config{RedirectOnSuccess: "/dashboard"}
	h, _ := cfg.CreateRoute("POST", "/login", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{"username": {"u"}, "password": {"p"}}))

	if rr.Code != http.StatusSeeOther {
		t.Fatalf("got %d, want 303", rr.Code)
	}
	if loc := rr.Header().Get("Location"); loc != "/dashboard" {
		t.Fatalf("Location=%q", loc)
	}
	sc := rr.Header().Get("Set-Cookie")
	if !strings.Contains(sc, "sid=tok-123") {
		t.Fatalf("cookie missing name=value: %q", sc)
	}
	if !strings.Contains(sc, "HttpOnly") {
		t.Fatalf("cookie not HttpOnly: %q", sc)
	}
	if !strings.Contains(sc, "Max-Age=3600") {
		t.Fatalf("cookie Max-Age wrong: %q", sc)
	}
}

func TestLogin_ExtraCookiesWritten(t *testing.T) {
	_, restore := stubLoginFn(t, &LoginResponse{
		Success: true, Location: "cookie", Name: "sid", Value: "x",
		ExtraCookies: []*http.Cookie{
			{Name: "saml_relay", Value: "deadbeef", Path: "/"},
		},
	})
	defer restore()

	h, _ := (&Config{}).CreateRoute("POST", "/login", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{"username": {"u"}, "password": {"p"}}))

	cookies := rr.Header().Values("Set-Cookie")
	if len(cookies) < 2 {
		t.Fatalf("expected at least 2 Set-Cookie headers (primary + extra), got %d: %v", len(cookies), cookies)
	}
	found := false
	for _, c := range cookies {
		if strings.Contains(c, "saml_relay=deadbeef") {
			found = true
		}
	}
	if !found {
		t.Fatalf("extra cookie not set: %v", cookies)
	}
}

func TestLogin_HeaderLocationWritesHeader(t *testing.T) {
	_, restore := stubLoginFn(t, &LoginResponse{
		Success: true, Location: "header", Name: "X-Auth", Value: "Bearer xyz",
	})
	defer restore()

	h, _ := (&Config{}).CreateRoute("POST", "/login", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{"username": {"u"}, "password": {"p"}}))

	if got := rr.Header().Get("X-Auth"); got != "Bearer xyz" {
		t.Fatalf("X-Auth=%q", got)
	}
	if ct := rr.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type=%q", ct)
	}
}

func TestLogin_FailureJSON(t *testing.T) {
	_, restore := stubLoginFn(t, &LoginResponse{
		Success: false, Error: "bad password", Code: "invalid_credentials",
		Details: map[string]string{"password": "too short"},
	})
	defer restore()

	cfg := &Config{ErrorResponseType: "json"}
	h, _ := cfg.CreateRoute("POST", "/login", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{"username": {"ada"}, "password": {"x"}}))

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{`"error":"bad password"`, `"code":"invalid_credentials"`, `"username":"ada"`} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestLogin_FailureRedirectFallbackToLogin(t *testing.T) {
	_, restore := stubLoginFn(t, &LoginResponse{Success: false, Error: "no"})
	defer restore()

	// Browser → redirect; no ErrorRedirect/RedirectOnFailure/Referer → /login
	req := formPost(url.Values{"username": {"u"}, "password": {"p"}})
	req.Header.Set("User-Agent", "Mozilla/5.0")
	rr := httptest.NewRecorder()
	h, _ := (&Config{}).CreateRoute("POST", "/login", nil)
	h(rr, req)

	if rr.Header().Get("Location") != "/login" {
		t.Fatalf("expected /login fallback, got %q", rr.Header().Get("Location"))
	}
}

func TestLogin_RequestAwareFnPreferred(t *testing.T) {
	prevFn := LoginFn
	prevReq := LoginFnWithRequest
	defer func() { LoginFn = prevFn; LoginFnWithRequest = prevReq }()

	LoginFn = func(u, p, n string) *LoginResponse {
		t.Fatalf("LoginFn should not be called when LoginFnWithRequest is set")
		return nil
	}
	calls := 0
	LoginFnWithRequest = func(u, p, n string, r *http.Request) *LoginResponse {
		calls++
		if r == nil {
			t.Fatal("request passed to LoginFnWithRequest is nil")
		}
		return &LoginResponse{Success: true, Location: "cookie", Name: "s"}
	}

	h, _ := (&Config{}).CreateRoute("POST", "/login", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{"username": {"u"}, "password": {"p"}}))

	if calls != 1 {
		t.Fatalf("LoginFnWithRequest should fire once, got %d", calls)
	}
}

func TestLogin_CookieSecureOverride(t *testing.T) {
	_, restore := stubLoginFn(t, &LoginResponse{Success: true, Location: "cookie", Name: "s", Value: "x"})
	defer restore()

	secure := true
	cfg := &Config{CookieSecure: &secure}
	h, _ := cfg.CreateRoute("POST", "/login", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{"username": {"u"}, "password": {"p"}}))

	if !strings.Contains(rr.Header().Get("Set-Cookie"), "Secure") {
		t.Fatalf("Secure not set: %q", rr.Header().Get("Set-Cookie"))
	}
}

func TestLogin_RejectsUnparseableForm(t *testing.T) {
	prevFn := LoginFn
	LoginFn = func(string, string, string) *LoginResponse {
		t.Fatal("LoginFn must not be called when form parse fails")
		return nil
	}
	defer func() { LoginFn = prevFn }()

	// Manually craft a request with a body that ParseForm rejects:
	// declared form content-type + non-percent-decodable body
	r := httptest.NewRequest("POST", "/login", strings.NewReader("a=%%xx"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h, _ := (&Config{}).CreateRoute("POST", "/login", nil)
	rr := httptest.NewRecorder()
	h(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
}
