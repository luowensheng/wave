package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeGitHub(t *testing.T, withPublicEmail bool) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/login/oauth/access_token", func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "gh_pat_" + r.PostForm.Get("code"),
		})
	})
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		if !strings.HasPrefix(auth, "token ") {
			t.Errorf("expected `token <T>` auth, got %q", auth)
		}
		payload := map[string]any{
			"id":         12345,
			"login":      "alice",
			"name":       "Alice",
			"avatar_url": "https://gh/a.png",
		}
		if withPublicEmail {
			payload["email"] = "alice-public@x.io"
		}
		_ = json.NewEncoder(w).Encode(payload)
	})
	mux.HandleFunc("/user/emails", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"email": "secondary@x.io", "primary": false, "verified": true},
			{"email": "alice@x.io", "primary": true, "verified": true},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestGitHubFullFlow(t *testing.T) {
	srv := fakeGitHub(t, false)
	p, err := newGitHub(Config{
		Provider: "github", ClientID: "cid", ClientSecret: "csec",
		AuthorizeURL: srv.URL + "/login/oauth/authorize",
		TokenURL:     srv.URL + "/login/oauth/access_token",
		UserinfoURL:  srv.URL + "/user",
	})
	if err != nil {
		t.Fatal(err)
	}
	tok, err := p.Exchange(context.Background(), "code-x", "https://app/cb")
	if err != nil {
		t.Fatal(err)
	}
	if tok != "gh_pat_code-x" {
		t.Errorf("token = %q", tok)
	}
	c, err := p.GetUserInfo(context.Background(), tok)
	if err != nil {
		t.Fatal(err)
	}
	// id was numeric in payload — should map to Subject as string.
	if c.Subject != "12345" {
		t.Errorf("subject = %q", c.Subject)
	}
	// Email should come from the secondary /user/emails call (no public email).
	if c.Email != "alice@x.io" || !c.EmailVerified {
		t.Errorf("email = %q verified=%v (expected primary from /user/emails)", c.Email, c.EmailVerified)
	}
	if c.Name != "Alice" || c.AvatarURL != "https://gh/a.png" {
		t.Errorf("claims = %+v", c)
	}
}

func TestGitHubUsesPublicEmailWhenPresent(t *testing.T) {
	srv := fakeGitHub(t, true)
	p, _ := newGitHub(Config{
		Provider: "github", ClientID: "x", ClientSecret: "y",
		TokenURL: srv.URL + "/login/oauth/access_token", UserinfoURL: srv.URL + "/user",
	})
	tok, _ := p.Exchange(context.Background(), "c", "https://x/cb")
	c, err := p.GetUserInfo(context.Background(), tok)
	if err != nil {
		t.Fatal(err)
	}
	if c.Email != "alice-public@x.io" {
		t.Errorf("email = %q", c.Email)
	}
}

func TestGitHubAuthorizeURLContainsParams(t *testing.T) {
	p, _ := newGitHub(Config{Provider: "github", ClientID: "cid", ClientSecret: "csec"})
	u := p.AuthorizeURL("nonce", "https://app/cb")
	for _, want := range []string{"client_id=cid", "state=nonce", "scope=read%3Auser+user%3Aemail"} {
		if !strings.Contains(u, want) {
			t.Errorf("missing %q in %s", want, u)
		}
	}
}

func TestGitHubRequiresCreds(t *testing.T) {
	if _, err := newGitHub(Config{}); err == nil {
		t.Error("expected error")
	}
}
