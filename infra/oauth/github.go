package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// github is the GitHub OAuth-2 provider. Quirks vs. generic:
//   - Authorization header uses `token <T>`, not `Bearer <T>`
//   - userinfo is at https://api.github.com/user
//   - Email is hidden by default unless the user marked it public; we
//     issue a secondary call to /user/emails to fetch the primary
//     verified address when the userinfo response omits it.
type github struct {
	cfg          Config
	client       *http.Client
	authorizeURL string
	tokenURL     string
	userURL      string
	emailsURL    string
}

func newGitHub(c Config) (Provider, error) {
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil, fmt.Errorf("oauth github: client_id and client_secret required")
	}
	if len(c.Scopes) == 0 {
		c.Scopes = []string{"read:user", "user:email"}
	}
	g := &github{
		cfg:          c,
		client:       &http.Client{Timeout: 10 * time.Second},
		authorizeURL: defaultIfEmpty(c.AuthorizeURL, "https://github.com/login/oauth/authorize"),
		tokenURL:     defaultIfEmpty(c.TokenURL, "https://github.com/login/oauth/access_token"),
		userURL:      defaultIfEmpty(c.UserinfoURL, "https://api.github.com/user"),
		emailsURL:    "https://api.github.com/user/emails",
	}
	// If the user pointed UserinfoURL elsewhere (test fake), derive
	// emailsURL from the same prefix so the primary-email fallback
	// hits the same fake server.
	if c.UserinfoURL != "" && strings.HasSuffix(c.UserinfoURL, "/user") {
		g.emailsURL = strings.TrimSuffix(c.UserinfoURL, "/user") + "/user/emails"
	}
	return g, nil
}

func (g *github) Name() string { return "github" }

func (g *github) AuthorizeURL(state, redirectURI string) string {
	v := url.Values{}
	v.Set("client_id", g.cfg.ClientID)
	v.Set("redirect_uri", redirectURI)
	v.Set("state", state)
	if len(g.cfg.Scopes) > 0 {
		v.Set("scope", strings.Join(g.cfg.Scopes, " "))
	}
	return g.authorizeURL + "?" + v.Encode()
}

func (g *github) Exchange(ctx context.Context, code, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("client_id", g.cfg.ClientID)
	form.Set("client_secret", g.cfg.ClientSecret)
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.tokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json") // GitHub defaults to form-encoded; JSON is friendlier
	resp, err := g.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("github token status %d: %s", resp.StatusCode, string(body))
	}
	var tr struct {
		AccessToken      string `json:"access_token"`
		Error            string `json:"error"`
		ErrorDescription string `json:"error_description"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("decode github token: %w", err)
	}
	if tr.Error != "" {
		return "", fmt.Errorf("github oauth error: %s (%s)", tr.Error, tr.ErrorDescription)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("github token response missing access_token")
	}
	return tr.AccessToken, nil
}

func (g *github) GetUserInfo(ctx context.Context, accessToken string) (*Claims, error) {
	raw, err := g.fetchJSON(ctx, accessToken, g.userURL)
	if err != nil {
		return nil, err
	}
	c := claimsFromMap(raw, "github")
	// GitHub uses `login` as the canonical username.
	if c.Name == "" {
		c.Name, _ = raw["login"].(string)
	}
	c.AvatarURL, _ = raw["avatar_url"].(string)

	// Public-email is often empty; pull primary verified email.
	if c.Email == "" {
		emails, err := g.fetchEmails(ctx, accessToken)
		if err == nil {
			for _, e := range emails {
				if e.Primary && e.Verified {
					c.Email = e.Email
					c.EmailVerified = true
					break
				}
			}
		}
	}
	return c, nil
}

func (g *github) fetchJSON(ctx context.Context, token, url string) (map[string]any, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("Authorization", "token "+token) // GitHub's quirk: not "Bearer"
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github %s status %d: %s", url, resp.StatusCode, string(body))
	}
	out := map[string]any{}
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	return out, nil
}

type ghEmail struct {
	Email    string `json:"email"`
	Primary  bool   `json:"primary"`
	Verified bool   `json:"verified"`
}

func (g *github) fetchEmails(ctx context.Context, token string) ([]ghEmail, error) {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, g.emailsURL, nil)
	req.Header.Set("Authorization", "token "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("github emails status %d", resp.StatusCode)
	}
	var out []ghEmail
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}
