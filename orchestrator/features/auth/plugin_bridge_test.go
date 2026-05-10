package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
)

// fakeAuthPlugin is an in-memory AuthPlugin used by the bridge tests.
type fakeAuthPlugin struct {
	mu             sync.Mutex
	authResult     *AuthPluginResult
	authErr        error
	logoutCalls    []string
	logoutErr      error
	authReqMethods []string
}

func (f *fakeAuthPlugin) Authenticate(_ context.Context, req *AuthPluginRequest) (*AuthPluginResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.authReqMethods = append(f.authReqMethods, req.Method)
	if f.authErr != nil {
		return nil, f.authErr
	}
	return f.authResult, nil
}

func (f *fakeAuthPlugin) RefreshClaims(_ context.Context, _ string) (*AuthPluginClaims, error) {
	return nil, errors.New("not implemented")
}

func (f *fakeAuthPlugin) Logout(_ context.Context, subject string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.logoutCalls = append(f.logoutCalls, subject)
	return f.logoutErr
}

// installFakeAuthPlugin sets up a fake AuthManager configured for one
// plugin-backed auth and returns the fake.
func installFakeAuthPlugin(t *testing.T, name string) *fakeAuthPlugin {
	t.Helper()
	fake := &fakeAuthPlugin{}
	cfg := &AuthConfig{
		Type:          "plugin",
		Plugin:        "the_plugin",
		TokenLocation: "cookie",
		CookieName:    "auth_token",
	}
	t.Setenv("SECRET_KEY", "test-secret-please-ignore")
	if err := InitAuthManager(map[string]*AuthConfig{name: cfg}); err != nil {
		t.Fatalf("InitAuthManager: %v", err)
	}
	RegisterAuthPlugins(map[string]AuthPlugin{name: fake})
	return fake
}

func TestPluginBridge_AuthenticateSuccess(t *testing.T) {
	fake := installFakeAuthPlugin(t, "saml")
	fake.authResult = &AuthPluginResult{
		Authenticated: true,
		Claims: &AuthPluginClaims{
			Subject: "alice@example.com",
			Email:   "alice@example.com",
			Roles:   []string{"admin"},
		},
		SetCookies: []*AuthPluginCookie{
			{Name: "saml_relay_state", Value: "abc", Path: "/"},
		},
	}

	resp := Login(LoginForm{}, "saml")
	if !resp.Success {
		t.Fatalf("expected success, got %+v", resp)
	}
	if resp.Value == "" {
		t.Fatal("expected JWT in resp.Value")
	}
	if resp.Name != "auth_token" {
		t.Fatalf("cookie name = %q, want auth_token", resp.Name)
	}
	if len(resp.ExtraCookies) != 1 || resp.ExtraCookies[0].Name != "saml_relay_state" {
		t.Fatalf("extra cookies = %+v, want saml_relay_state", resp.ExtraCookies)
	}
}

func TestPluginBridge_AuthenticateFailure(t *testing.T) {
	fake := installFakeAuthPlugin(t, "saml")
	fake.authResult = &AuthPluginResult{Authenticated: false}

	resp := Login(LoginForm{}, "saml")
	if resp.Success {
		t.Fatal("expected failure")
	}
	if resp.Error == "" {
		t.Fatal("expected error message")
	}
}

func TestPluginBridge_RedirectOverride(t *testing.T) {
	fake := installFakeAuthPlugin(t, "saml")
	fake.authResult = &AuthPluginResult{
		Authenticated: true,
		Claims:        &AuthPluginClaims{Subject: "bob"},
		Redirect:      "/from-plugin",
	}
	resp := Login(LoginForm{}, "saml")
	if !resp.Success {
		t.Fatalf("expected success, got %+v", resp)
	}
	if resp.RedirectTo != "/from-plugin" {
		t.Fatalf("RedirectTo = %q, want /from-plugin", resp.RedirectTo)
	}
}

func TestPluginBridge_LogoutCallsPlugin(t *testing.T) {
	fake := installFakeAuthPlugin(t, "saml")
	r := httptest.NewRequest("POST", "/logout", nil)

	// Plugin logout is best-effort so we don't assert on the response
	// success — we only require the plugin's Logout was invoked.
	_ = Logout(r, "saml")
	if len(fake.logoutCalls) != 1 {
		t.Fatalf("logout calls = %d, want 1", len(fake.logoutCalls))
	}
}

func TestPluginBridge_LoginWithRequestThreadsHeaders(t *testing.T) {
	fake := installFakeAuthPlugin(t, "saml")
	fake.authResult = &AuthPluginResult{
		Authenticated: true,
		Claims:        &AuthPluginClaims{Subject: "carol"},
	}
	r := httptest.NewRequest("POST", "/login", nil)
	r.Header.Set("X-Auth-Method", "saml_callback")
	r.AddCookie(&http.Cookie{Name: "session_seed", Value: "xyz"})

	resp := LoginWithRequest(LoginForm{}, "saml", r)
	if !resp.Success {
		t.Fatalf("expected success: %+v", resp)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.authReqMethods) != 1 || fake.authReqMethods[0] != "saml_callback" {
		t.Fatalf("plugin saw method = %v, want [saml_callback]", fake.authReqMethods)
	}
}
