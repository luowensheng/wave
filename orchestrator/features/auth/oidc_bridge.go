package auth

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"wave/domain"
	"wave/infra/oidc"
	"wave/infra/rbac"
)

// oidcVerifiers holds one Verifier per AuthConfig of Type == "oidc".
// Built once at boot from setupOIDC() so per-request authenticate()
// just does a map lookup + verify.
var (
	oidcMu        sync.RWMutex
	oidcVerifiers = map[string]*oidc.Verifier{}
)

// setupOIDC walks the configured auth blocks and constructs a Verifier
// for each Type == "oidc" entry. Boot-time errors fail fast — that's
// the whole point of OIDC discovery happening once at startup.
func setupOIDC(configs map[string]*AuthConfig) error {
	for key, cfg := range configs {
		if cfg == nil || strings.ToLower(cfg.Type) != "oidc" {
			continue
		}
		if cfg.Issuer == "" || cfg.ClientID == "" {
			return fmt.Errorf("auth %q: oidc requires `issuer` and `client_id`", key)
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		v, err := oidc.New(ctx, oidc.Config{
			Issuer:   cfg.Issuer,
			ClientID: cfg.ClientID,
		})
		cancel()
		if err != nil {
			return fmt.Errorf("auth %q: oidc init: %w", key, err)
		}
		oidcMu.Lock()
		oidcVerifiers[key] = v
		oidcMu.Unlock()
		log.Printf("[INFO] OIDC verifier initialized: name=%s issuer=%s", key, cfg.Issuer)
	}
	return nil
}

// authenticateOIDC reads the bearer token, verifies it against the
// per-config OIDC verifier, and adapts the resulting Claims into a
// domain.User. The User has a negative ID (matching the "default user"
// convention) since OIDC users aren't backed by the local user store.
func (am *AuthManager) authenticateOIDC(r *http.Request, config *AuthConfig) (*User, error) {
	oidcMu.RLock()
	v, ok := oidcVerifiers[config.key]
	oidcMu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("oidc verifier not initialized for %q", config.key)
	}

	token, err := am.extractToken(r, config)
	if err != nil {
		return nil, fmt.Errorf("oidc token extraction: %w", err)
	}
	claims, err := v.Verify(r.Context(), token)
	if err != nil {
		return nil, fmt.Errorf("oidc verify: %w", err)
	}
	username := claims.Email
	if username == "" {
		username = claims.Subject
	}
	// Thread the raw claims onto the request context so downstream RBAC
	// middleware (require_roles / require_claims) can read them. We
	// mutate r in place via WithContext — caller (RequireAuth) doesn't
	// care because the next call replaces r anyway with a user-context
	// version, and rbac.WithClaims uses a private key so collisions are
	// impossible.
	*r = *r.WithContext(rbac.WithClaims(r.Context(), claims.Map))
	return &domain.User{
		ID:        -1, // OIDC users live outside the local user store
		Username:  username,
		IsDefault: true,
	}, nil
}
