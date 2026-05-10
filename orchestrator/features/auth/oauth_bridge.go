package auth

import (
	"fmt"
	"log"
	"strings"

	"wave/infra/oauth"
	oauthrt "wave/usecases/oauth_routes"
)

// setupOAuth walks the configured auth blocks and builds an
// oauth.Provider for each Type == "oauth" entry, registering it under
// the auth-config name in usecases/oauth_routes so the route handlers
// can look it up at request time.
//
// Mirrors setupOIDC in oidc_bridge.go. Boot-time errors fail fast.
func setupOAuth(configs map[string]*AuthConfig) error {
	for name, cfg := range configs {
		if cfg == nil || strings.ToLower(cfg.Type) != "oauth" {
			continue
		}
		if cfg.OAuth == nil || cfg.OAuth.Provider == "" {
			return fmt.Errorf("auth %q: oauth requires `oauth: { provider: ... }`", name)
		}
		p, err := oauth.Build(oauth.Config{
			Provider:            cfg.OAuth.Provider,
			ClientID:            cfg.OAuth.ClientID,
			ClientSecret:        cfg.OAuth.ClientSecret,
			Scopes:              cfg.OAuth.Scopes,
			AuthorizeURL:        cfg.OAuth.AuthorizeURL,
			TokenURL:            cfg.OAuth.TokenURL,
			UserinfoURL:         cfg.OAuth.UserinfoURL,
			AppleTeamID:         cfg.OAuth.AppleTeamID,
			AppleKeyID:          cfg.OAuth.AppleKeyID,
			ApplePrivateKeyPath: cfg.OAuth.ApplePrivateKeyPath,
			ApplePrivateKeyPEM:  cfg.OAuth.ApplePrivateKeyPEM,
		})
		if err != nil {
			return fmt.Errorf("auth %q: oauth init: %w", name, err)
		}
		oauthrt.SetProvider(name, p)
		log.Printf("[INFO] OAuth provider initialized: name=%s provider=%s",
			name, cfg.OAuth.Provider)
	}
	return nil
}
