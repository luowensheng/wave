package oauth_routes

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/luowensheng/wave/infra/oauth"
)

// fakeProvider lets us exercise the route handlers without booting a
// real OAuth IdP.
type fakeProvider struct {
	auth     string
	exchange func(code string) (string, error)
	user     func(tok string) (*oauth.Claims, error)
}

func (f *fakeProvider) Name() string { return "fake" }
func (f *fakeProvider) AuthorizeURL(state, redirect string) string {
	return f.auth + "?state=" + state + "&redirect_uri=" + redirect
}
func (f *fakeProvider) Exchange(_ context.Context, code, _ string) (string, error) {
	return f.exchange(code)
}
func (f *fakeProvider) GetUserInfo(_ context.Context, tok string) (*oauth.Claims, error) {
	return f.user(tok)
}

func TestStartRedirectsToAuthorizeURLWithState(t *testing.T) {
	SetProvider("github", &fakeProvider{auth: "https://github/auth"})
	cfg := &StartConfig{AuthConfig: "github", RedirectURI: "https://app/cb"}
	h, err := cfg.CreateRoute("GET", "/login", nil)
	if err != nil {
		t.Fatal(err)
	}
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest("GET", "/login", nil))
	if w.Code != http.StatusFound {
		t.Fatalf("status = %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://github/auth?state=") {
		t.Errorf("location = %q", loc)
	}
	// The state cookie was set.
	if !strings.Contains(w.Header().Get("Set-Cookie"), stateCookie) {
		t.Errorf("state cookie not set: %v", w.Header().Get("Set-Cookie"))
	}
}

func TestCallbackFullFlow(t *testing.T) {
	loggedIn := atomic.Int32{}
	var loggedClaims *oauth.Claims
	SetLoginFn(func(_ context.Context, c *oauth.Claims, w http.ResponseWriter, r *http.Request) error {
		loggedIn.Add(1)
		loggedClaims = c
		return nil
	})
	SetProvider("github", &fakeProvider{
		exchange: func(code string) (string, error) { return "tok-" + code, nil },
		user: func(tok string) (*oauth.Claims, error) {
			return &oauth.Claims{Subject: "u1", Email: "alice@x.io", Provider: "github"}, nil
		},
	})

	cfg := &CallbackConfig{
		AuthConfig: "github", RedirectURI: "https://app/cb",
		SuccessRedirect: "/dashboard",
	}
	h, _ := cfg.CreateRoute("GET", "/cb", nil)

	r := httptest.NewRequest("GET", "/cb?code=abc&state=nonce-1", nil)
	r.AddCookie(&http.Cookie{Name: stateCookie, Value: "nonce-1"})
	w := httptest.NewRecorder()
	h(w, r)

	if w.Code != http.StatusFound {
		t.Fatalf("status = %d body=%q", w.Code, w.Body.String())
	}
	if w.Header().Get("Location") != "/dashboard" {
		t.Errorf("location = %q", w.Header().Get("Location"))
	}
	if loggedIn.Load() != 1 {
		t.Error("login fn not invoked")
	}
	if loggedClaims == nil || loggedClaims.Email != "alice@x.io" {
		t.Errorf("claims = %+v", loggedClaims)
	}
}

func TestCallbackStateMismatchFails(t *testing.T) {
	SetProvider("github", &fakeProvider{
		exchange: func(string) (string, error) { return "tok", nil },
		user:     func(string) (*oauth.Claims, error) { return &oauth.Claims{}, nil },
	})
	cfg := &CallbackConfig{
		AuthConfig: "github", RedirectURI: "https://app/cb",
		FailureRedirect: "/login?err=state",
	}
	h, _ := cfg.CreateRoute("GET", "/cb", nil)

	r := httptest.NewRequest("GET", "/cb?code=abc&state=wrong", nil)
	r.AddCookie(&http.Cookie{Name: stateCookie, Value: "right"})
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != http.StatusFound || w.Header().Get("Location") != "/login?err=state" {
		t.Errorf("status=%d location=%q", w.Code, w.Header().Get("Location"))
	}
}

func TestCallbackMissingStateCookieFails(t *testing.T) {
	cfg := &CallbackConfig{AuthConfig: "github", RedirectURI: "https://x/cb"}
	h, _ := cfg.CreateRoute("GET", "/cb", nil)
	w := httptest.NewRecorder()
	h(w, httptest.NewRequest("GET", "/cb?code=x&state=y", nil))
	if w.Code != http.StatusUnauthorized {
		t.Errorf("status = %d", w.Code)
	}
}

func TestCallbackExchangeErrorFails(t *testing.T) {
	SetProvider("p", &fakeProvider{
		exchange: func(string) (string, error) { return "", http.ErrAbortHandler },
		user:     func(string) (*oauth.Claims, error) { return &oauth.Claims{}, nil },
	})
	cfg := &CallbackConfig{
		AuthConfig: "p", RedirectURI: "https://x/cb",
		FailureRedirect: "/login?err=exchange",
	}
	h, _ := cfg.CreateRoute("GET", "/cb", nil)
	r := httptest.NewRequest("GET", "/cb?code=x&state=s", nil)
	r.AddCookie(&http.Cookie{Name: stateCookie, Value: "s"})
	w := httptest.NewRecorder()
	h(w, r)
	if w.Header().Get("Location") != "/login?err=exchange" {
		t.Errorf("location = %q", w.Header().Get("Location"))
	}
}

func TestStartValidates(t *testing.T) {
	if _, err := (&StartConfig{}).CreateRoute("GET", "/", nil); err == nil {
		t.Error("expected error")
	}
	if _, err := (&StartConfig{AuthConfig: "x"}).CreateRoute("GET", "/", nil); err == nil {
		t.Error("expected error for missing redirect_uri")
	}
}

func TestCallbackJSONResponseWhenNoSuccessRedirect(t *testing.T) {
	SetLoginFn(func(_ context.Context, _ *oauth.Claims, _ http.ResponseWriter, _ *http.Request) error {
		return nil
	})
	SetProvider("p", &fakeProvider{
		exchange: func(string) (string, error) { return "tok", nil },
		user: func(string) (*oauth.Claims, error) {
			return &oauth.Claims{Subject: "u", Email: "e@x"}, nil
		},
	})
	cfg := &CallbackConfig{AuthConfig: "p", RedirectURI: "https://x/cb"}
	h, _ := cfg.CreateRoute("GET", "/cb", nil)
	r := httptest.NewRequest("GET", "/cb?code=c&state=n", nil)
	r.AddCookie(&http.Cookie{Name: stateCookie, Value: "n"})
	w := httptest.NewRecorder()
	h(w, r)
	if w.Code != 200 {
		t.Fatalf("status = %d", w.Code)
	}
	var got map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got["ok"] != true || got["email"] != "e@x" {
		t.Errorf("body = %v", got)
	}
}
