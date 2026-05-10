// Package oauth defines the Provider interface for OAuth-2 sign-in
// flows (Apple, GitHub, generic OAuth-2 IdPs that don't speak OIDC).
//
// OIDC (in infra/oidc) already covers Google / Okta / Auth0 / Entra
// — for those, prefer OIDC. This package fills the gap for IdPs that
// don't expose OIDC discovery: Apple's signed-JWT client_secret flow,
// GitHub's bare-OAuth-2 + custom userinfo endpoint, and any other
// provider you want to wire by satisfying the 3-method interface.
//
// The "plugin" framing the user asked about: each provider is a Go
// type implementing Provider. Built-ins ship in this package; users
// register additional providers via Register() — the AuthConfig's
// `provider:` string then resolves through the registry at boot.
package oauth

import (
	"context"
	"fmt"
	"sync"
)

// Claims is the normalized identity returned by GetUserInfo. Every
// provider maps its raw payload into these fields plus stashes the
// full payload in Raw for callers that need provider-specific bits.
type Claims struct {
	Subject       string         // unique per-provider identity
	Email         string
	EmailVerified bool
	Name          string
	AvatarURL     string
	Provider      string         // "google_oauth", "github", "apple", custom
	Raw           map[string]any // full provider-specific userinfo payload
}

// Provider is the contract every OAuth provider satisfies.
type Provider interface {
	// Name uniquely identifies the provider (used in audit + Claims.Provider).
	Name() string

	// AuthorizeURL returns the URL the browser should be redirected to
	// for the user-consent step. `state` is a CSRF-bound nonce the
	// caller should also store in a cookie for later verification.
	AuthorizeURL(state, redirectURI string) string

	// Exchange swaps the callback `code` for an access token.
	Exchange(ctx context.Context, code, redirectURI string) (accessToken string, err error)

	// GetUserInfo fetches and normalizes the user identity given an
	// access token returned by Exchange.
	GetUserInfo(ctx context.Context, accessToken string) (*Claims, error)
}

// Config is the YAML-side description of one OAuth provider config.
// Different providers use different subsets — Apple needs the .p8
// key, GitHub needs nothing extra, etc.
type Config struct {
	Provider     string   // "apple" | "github" | "google_oauth" | "generic" | <custom>
	ClientID     string
	ClientSecret string   // unused by Apple (uses signed JWT instead)
	Scopes       []string

	// Generic provider only — required for unknown IdPs.
	AuthorizeURL string
	TokenURL     string
	UserinfoURL  string

	// Apple-specific.
	AppleTeamID         string
	AppleKeyID          string
	ApplePrivateKeyPath string // path to .p8 file
	ApplePrivateKeyPEM  string // alternative: inline PEM (use ${FILE:...})
}

// Constructor builds a Provider from a Config. Registered constructors
// come from this package and from user code (via Register).
type Constructor func(cfg Config) (Provider, error)

var (
	regMu sync.RWMutex
	reg   = map[string]Constructor{}
)

// Register makes a constructor discoverable by name. Idempotent on
// name: re-registration overwrites (last wins). Call from init() in
// any package that defines a custom provider — auth.AuthConfig.OAuth
// will resolve through this registry at server boot.
func Register(name string, c Constructor) {
	regMu.Lock()
	defer regMu.Unlock()
	reg[name] = c
}

// Build constructs a Provider for the given config, looking the
// constructor up in the registry by Config.Provider.
func Build(cfg Config) (Provider, error) {
	regMu.RLock()
	c, ok := reg[cfg.Provider]
	regMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("oauth: unknown provider %q (registered: %v)", cfg.Provider, registeredNames())
	}
	return c(cfg)
}

func registeredNames() []string {
	regMu.RLock()
	defer regMu.RUnlock()
	out := make([]string, 0, len(reg))
	for n := range reg {
		out = append(out, n)
	}
	return out
}

// init registers the four built-in providers. Each lives in its own
// file so the import-graph stays tidy.
func init() {
	Register("generic", func(c Config) (Provider, error) { return newGeneric(c) })
	Register("google_oauth", func(c Config) (Provider, error) { return newGoogle(c) })
	Register("github", func(c Config) (Provider, error) { return newGitHub(c) })
	Register("apple", func(c Config) (Provider, error) { return newApple(c) })
}
