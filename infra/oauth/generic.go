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

// generic implements OAuth-2 against an IdP that uses the standard
// authorize/token/userinfo flow. Configurable everything; baseline
// for any provider that doesn't have a built-in helper.
type generic struct {
	cfg    Config
	client *http.Client
}

func newGeneric(c Config) (*generic, error) {
	if c.AuthorizeURL == "" || c.TokenURL == "" || c.UserinfoURL == "" {
		return nil, fmt.Errorf("oauth generic: authorize_url, token_url, userinfo_url all required")
	}
	if c.ClientID == "" || c.ClientSecret == "" {
		return nil, fmt.Errorf("oauth generic: client_id and client_secret required")
	}
	return &generic{cfg: c, client: &http.Client{Timeout: 10 * time.Second}}, nil
}

func (g *generic) Name() string { return "generic" }

func (g *generic) AuthorizeURL(state, redirectURI string) string {
	v := url.Values{}
	v.Set("response_type", "code")
	v.Set("client_id", g.cfg.ClientID)
	v.Set("redirect_uri", redirectURI)
	v.Set("state", state)
	if len(g.cfg.Scopes) > 0 {
		v.Set("scope", strings.Join(g.cfg.Scopes, " "))
	}
	sep := "?"
	if strings.Contains(g.cfg.AuthorizeURL, "?") {
		sep = "&"
	}
	return g.cfg.AuthorizeURL + sep + v.Encode()
}

func (g *generic) Exchange(ctx context.Context, code, redirectURI string) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", redirectURI)
	form.Set("client_id", g.cfg.ClientID)
	form.Set("client_secret", g.cfg.ClientSecret)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.cfg.TokenURL, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("token request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("token endpoint status %d: %s", resp.StatusCode, string(body))
	}
	var tr struct {
		AccessToken string `json:"access_token"`
		Error       string `json:"error"`
	}
	if err := json.Unmarshal(body, &tr); err != nil {
		return "", fmt.Errorf("decode token response: %w", err)
	}
	if tr.Error != "" {
		return "", fmt.Errorf("token error: %s", tr.Error)
	}
	if tr.AccessToken == "" {
		return "", fmt.Errorf("token response missing access_token")
	}
	return tr.AccessToken, nil
}

func (g *generic) GetUserInfo(ctx context.Context, accessToken string) (*Claims, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.cfg.UserinfoURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+accessToken)
	req.Header.Set("Accept", "application/json")
	resp, err := g.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("userinfo request: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("userinfo status %d: %s", resp.StatusCode, string(body))
	}
	raw := map[string]any{}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode userinfo: %w", err)
	}
	return claimsFromMap(raw, "generic"), nil
}

// claimsFromMap pulls the OIDC-standard fields out of a userinfo map.
// Providers with quirky shapes override individual fields after.
func claimsFromMap(raw map[string]any, provider string) *Claims {
	c := &Claims{Raw: raw, Provider: provider}
	c.Subject, _ = raw["sub"].(string)
	if c.Subject == "" {
		// Some providers use "id" or numeric IDs.
		switch v := raw["id"].(type) {
		case string:
			c.Subject = v
		case float64:
			c.Subject = fmt.Sprintf("%d", int64(v))
		}
	}
	c.Email, _ = raw["email"].(string)
	c.EmailVerified, _ = raw["email_verified"].(bool)
	c.Name, _ = raw["name"].(string)
	if c.Name == "" {
		// GitHub etc. use "login".
		c.Name, _ = raw["login"].(string)
	}
	c.AvatarURL, _ = raw["picture"].(string)
	if c.AvatarURL == "" {
		c.AvatarURL, _ = raw["avatar_url"].(string)
	}
	return c
}
