package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeIDP returns a tiny OAuth-2 IdP for tests. Behavior:
//   /authorize → not actually invoked by the package (browser redirect)
//   /token     → returns {access_token: "tok-<code>"} for any code != "bad"
//   /userinfo  → returns a fixed payload, requires Bearer header
func fakeIDP(t *testing.T) (*httptest.Server, *atomic.Int64, *atomic.Int64) {
	t.Helper()
	tokenCalls := &atomic.Int64{}
	userCalls := &atomic.Int64{}
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		tokenCalls.Add(1)
		_ = r.ParseForm()
		if r.PostForm.Get("code") == "bad" {
			http.Error(w, `{"error":"invalid_grant"}`, 400)
			return
		}
		if r.PostForm.Get("client_id") == "" || r.PostForm.Get("client_secret") == "" {
			t.Errorf("missing creds: %v", r.PostForm)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-" + r.PostForm.Get("code"),
		})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		userCalls.Add(1)
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "missing bearer", 401)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"sub":            "user-42",
			"email":          "u@x.io",
			"email_verified": true,
			"name":           "Alice",
			"picture":        "https://x.io/a.png",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, tokenCalls, userCalls
}

func TestGenericAuthorizeURL(t *testing.T) {
	srv, _, _ := fakeIDP(t)
	p, err := newGeneric(Config{
		Provider: "generic", ClientID: "cid", ClientSecret: "csec",
		AuthorizeURL: srv.URL + "/authorize", TokenURL: srv.URL + "/token", UserinfoURL: srv.URL + "/userinfo",
		Scopes: []string{"read", "write"},
	})
	if err != nil {
		t.Fatal(err)
	}
	u := p.AuthorizeURL("xyz", "https://app.example.com/cb")
	for _, want := range []string{"client_id=cid", "state=xyz", "response_type=code",
		"redirect_uri=https%3A%2F%2Fapp.example.com%2Fcb", "scope=read+write"} {
		if !strings.Contains(u, want) {
			t.Errorf("missing %q in %s", want, u)
		}
	}
}

func TestGenericFullFlow(t *testing.T) {
	srv, tokenCalls, userCalls := fakeIDP(t)
	p, _ := newGeneric(Config{
		Provider: "generic", ClientID: "cid", ClientSecret: "csec",
		AuthorizeURL: srv.URL + "/authorize", TokenURL: srv.URL + "/token", UserinfoURL: srv.URL + "/userinfo",
	})
	tok, err := p.Exchange(context.Background(), "code-123", "https://app.example.com/cb")
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if tok != "tok-code-123" {
		t.Errorf("token = %q", tok)
	}
	c, err := p.GetUserInfo(context.Background(), tok)
	if err != nil {
		t.Fatalf("userinfo: %v", err)
	}
	if c.Subject != "user-42" || c.Email != "u@x.io" || c.Name != "Alice" {
		t.Errorf("claims = %+v", c)
	}
	if !c.EmailVerified {
		t.Error("EmailVerified should be true")
	}
	if c.AvatarURL != "https://x.io/a.png" {
		t.Errorf("AvatarURL = %q", c.AvatarURL)
	}
	if tokenCalls.Load() != 1 || userCalls.Load() != 1 {
		t.Errorf("calls: token=%d user=%d", tokenCalls.Load(), userCalls.Load())
	}
}

func TestGenericExchangeErrorPropagates(t *testing.T) {
	srv, _, _ := fakeIDP(t)
	p, _ := newGeneric(Config{
		Provider: "generic", ClientID: "cid", ClientSecret: "csec",
		AuthorizeURL: srv.URL, TokenURL: srv.URL + "/token", UserinfoURL: srv.URL + "/userinfo",
	})
	if _, err := p.Exchange(context.Background(), "bad", "https://x/cb"); err == nil {
		t.Error("expected error")
	}
}

func TestGenericValidatesConfig(t *testing.T) {
	if _, err := newGeneric(Config{}); err == nil {
		t.Error("expected error for missing URLs")
	}
	if _, err := newGeneric(Config{
		AuthorizeURL: "x", TokenURL: "x", UserinfoURL: "x",
	}); err == nil {
		t.Error("expected error for missing client creds")
	}
}

func TestRegistryBuildsBuiltins(t *testing.T) {
	if _, err := Build(Config{Provider: "generic", ClientID: "x", ClientSecret: "y",
		AuthorizeURL: "https://a", TokenURL: "https://t", UserinfoURL: "https://u"}); err != nil {
		t.Errorf("generic build: %v", err)
	}
	if _, err := Build(Config{Provider: "github", ClientID: "x", ClientSecret: "y"}); err != nil {
		t.Errorf("github build: %v", err)
	}
	if _, err := Build(Config{Provider: "google_oauth", ClientID: "x", ClientSecret: "y"}); err != nil {
		t.Errorf("google build: %v", err)
	}
	if _, err := Build(Config{Provider: "no_such_provider"}); err == nil {
		t.Error("unknown provider should error")
	}
}

func TestRegisterCustomProvider(t *testing.T) {
	Register("test_custom", func(c Config) (Provider, error) {
		return &generic{cfg: c, client: nil}, nil
	})
	if _, err := Build(Config{Provider: "test_custom"}); err != nil {
		t.Errorf("custom provider build: %v", err)
	}
}
