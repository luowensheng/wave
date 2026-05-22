package auth_signup

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

type signupCall struct {
	username, password, confirm, name string
}

func stubSignupFn(t *testing.T, resp *LoginResponse) (calls *[]signupCall, restore func()) {
	t.Helper()
	prev := SignupFn
	c := []signupCall{}
	SignupFn = func(u, p, cp, n string) *LoginResponse {
		c = append(c, signupCall{u, p, cp, n})
		return resp
	}
	return &c, func() { SignupFn = prev }
}

func stubLoginFn(t *testing.T, resp *LoginResponse) (calls *int, restore func()) {
	t.Helper()
	prev := LoginFn
	n := 0
	LoginFn = func(u, p, name string) *LoginResponse {
		n++
		return resp
	}
	return &n, func() { LoginFn = prev }
}

func formPost(body url.Values) *http.Request {
	r := httptest.NewRequest("POST", "/signup", strings.NewReader(body.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return r
}

func TestSignup_NotConfigured(t *testing.T) {
	prev := SignupFn
	SignupFn = nil
	defer func() { SignupFn = prev }()

	cfg := &Config{ErrorResponseType: "json"}
	h, _ := cfg.CreateRoute("POST", "/signup", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{"username": {"u"}, "password": {"p"}}))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "not_configured") {
		t.Fatalf("body=%q", rr.Body.String())
	}
}

func TestSignup_PassesAllFieldsToSignupFn(t *testing.T) {
	calls, restore := stubSignupFn(t, &LoginResponse{
		Success: true, Username: "ada",
	})
	defer restore()

	cfg := &Config{For: "primary"}
	h, _ := cfg.CreateRoute("POST", "/signup", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{
		"username":         {"ada"},
		"password":         {"hunter2"},
		"confirm_password": {"hunter2"},
		"email":            {"ada@example.com"},
	}))

	if len(*calls) != 1 {
		t.Fatalf("expected 1 SignupFn call, got %d", len(*calls))
	}
	c := (*calls)[0]
	if c.username != "ada" || c.password != "hunter2" || c.confirm != "hunter2" || c.name != "primary" {
		t.Fatalf("got call %+v", c)
	}
	_ = rr
}

func TestSignup_CustomFieldNames(t *testing.T) {
	calls, restore := stubSignupFn(t, &LoginResponse{Success: true})
	defer restore()

	cfg := &Config{
		UsernameField:        "user",
		PasswordField:        "pw",
		ConfirmPasswordField: "pw2",
		EmailField:           "addr",
	}
	h, _ := cfg.CreateRoute("POST", "/signup", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{
		"user": {"ada"}, "pw": {"x"}, "pw2": {"x"}, "addr": {"a@b.com"},
	}))

	c := (*calls)[0]
	if c.username != "ada" || c.password != "x" || c.confirm != "x" {
		t.Fatalf("custom fields not honored: %+v", c)
	}
	_ = rr
}

func TestSignup_SuccessNoAutoLoginReturns201JSON(t *testing.T) {
	_, restore := stubSignupFn(t, &LoginResponse{
		Success: true, Username: "ada", UserID: 42,
	})
	defer restore()

	cfg := &Config{}
	h, _ := cfg.CreateRoute("POST", "/signup", nil)
	rr := httptest.NewRecorder()
	// API client (no Mozilla / text/html)
	h(rr, formPost(url.Values{"username": {"ada"}, "password": {"x"}}))

	if rr.Code != http.StatusCreated {
		t.Fatalf("got %d, want 201", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), `"Username":"ada"`) {
		t.Fatalf("body=%q", rr.Body.String())
	}
}

func TestSignup_AutoLoginCallsLoginFn(t *testing.T) {
	_, restoreSignup := stubSignupFn(t, &LoginResponse{Success: true, Username: "ada"})
	defer restoreSignup()

	loginCalls, restoreLogin := stubLoginFn(t, &LoginResponse{
		Success: true, Location: "cookie", Name: "sid", Value: "tok",
	})
	defer restoreLogin()

	cfg := &Config{AutoLogin: true, RedirectOnSuccess: "/welcome"}
	h, _ := cfg.CreateRoute("POST", "/signup", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{"username": {"ada"}, "password": {"x"}, "confirm_password": {"x"}}))

	if *loginCalls != 1 {
		t.Fatalf("expected LoginFn to fire once after auto-login, got %d", *loginCalls)
	}
	if !strings.Contains(rr.Header().Get("Set-Cookie"), "sid=tok") {
		t.Fatalf("cookie missing: %q", rr.Header().Get("Set-Cookie"))
	}
	if rr.Header().Get("Location") != "/welcome" {
		t.Fatalf("Location=%q", rr.Header().Get("Location"))
	}
}

func TestSignup_AutoLoginFailureFallsThroughToRedirect(t *testing.T) {
	_, restoreSignup := stubSignupFn(t, &LoginResponse{Success: true, Username: "ada"})
	defer restoreSignup()
	_, restoreLogin := stubLoginFn(t, &LoginResponse{Success: false, Error: "boom"})
	defer restoreLogin()

	cfg := &Config{AutoLogin: true, RedirectOnSuccess: "/welcome"}
	h, _ := cfg.CreateRoute("POST", "/signup", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{"username": {"ada"}, "password": {"x"}}))

	// Should still redirect to success even though auto-login bailed.
	if rr.Header().Get("Location") != "/welcome" {
		t.Fatalf("Location=%q", rr.Header().Get("Location"))
	}
}

func TestSignup_FailureJSONIncludesFieldDetails(t *testing.T) {
	_, restore := stubSignupFn(t, &LoginResponse{
		Success: false, Error: "weak", Code: "password_policy",
		Details: map[string]string{"password": "must include a digit"},
	})
	defer restore()

	cfg := &Config{ErrorResponseType: "json"}
	h, _ := cfg.CreateRoute("POST", "/signup", nil)
	rr := httptest.NewRecorder()
	h(rr, formPost(url.Values{
		"username": {"ada"}, "password": {"abc"}, "email": {"a@b.com"},
	}))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{
		`"error":"weak"`,
		`"code":"password_policy"`,
		`"username":"ada"`,
		`"email":"a@b.com"`,
		`"password":"must include a digit"`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("body missing %q: %s", want, body)
		}
	}
}

func TestSignup_RejectsUnparseableForm(t *testing.T) {
	prev := SignupFn
	SignupFn = func(_, _, _, _ string) *LoginResponse {
		t.Fatal("SignupFn must not be called when form parse fails")
		return nil
	}
	defer func() { SignupFn = prev }()

	r := httptest.NewRequest("POST", "/signup", strings.NewReader("a=%%xx"))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	h, _ := (&Config{}).CreateRoute("POST", "/signup", nil)
	rr := httptest.NewRecorder()
	h(rr, r)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("got %d", rr.Code)
	}
}
