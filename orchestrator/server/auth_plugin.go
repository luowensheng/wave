// Package servers — auth_plugin wires plugin-backed auth backends into
// the server boot. Mirrors storage_plugin.go: the orchestrator owns
// sessions/cookies/JWTs; kind=auth plugins only provide identity. See
// docs/auth-plugins.md.
package servers

import (
	"context"
	"fmt"
	"strings"

	authfeature "wave/orchestrator/features/auth"
	"wave/infra/plugins"
	"wave/infra/plugins/kinds"
)

// authPluginAdapter adapts the kind-package AuthPlugin (which depends on
// wave/infra/plugins/kinds and friends) into the local
// authfeature.AuthPlugin interface — bridging the import cycle that
// would otherwise exist via io/http/contentloader.
type authPluginAdapter struct {
	inner kinds.AuthPlugin
}

func (a *authPluginAdapter) Authenticate(ctx context.Context, req *authfeature.AuthPluginRequest) (*authfeature.AuthPluginResult, error) {
	res, err := a.inner.Authenticate(ctx, &kinds.AuthRequest{
		Method:      req.Method,
		Credentials: req.Credentials,
		Headers:     req.Headers,
		Cookies:     req.Cookies,
	})
	if err != nil {
		return nil, err
	}
	if res == nil {
		return nil, nil
	}
	out := &authfeature.AuthPluginResult{
		Authenticated: res.Authenticated,
		Redirect:      res.Redirect,
	}
	if res.Claims != nil {
		out.Claims = &authfeature.AuthPluginClaims{
			Subject:       res.Claims.Subject,
			Email:         res.Claims.Email,
			EmailVerified: res.Claims.EmailVerified,
			Name:          res.Claims.Name,
			Roles:         res.Claims.Roles,
			Scopes:        res.Claims.Scopes,
			Provider:      res.Claims.Provider,
			Raw:           res.Claims.Raw,
		}
	}
	for _, c := range res.SetCookies {
		if c == nil {
			continue
		}
		out.SetCookies = append(out.SetCookies, &authfeature.AuthPluginCookie{
			Name: c.Name, Value: c.Value, Path: c.Path, Domain: c.Domain,
			MaxAge: c.MaxAge, Secure: c.Secure, HTTPOnly: c.HTTPOnly,
			SameSite: c.SameSite,
		})
	}
	return out, nil
}

func (a *authPluginAdapter) RefreshClaims(ctx context.Context, subject string) (*authfeature.AuthPluginClaims, error) {
	c, err := a.inner.RefreshClaims(ctx, subject)
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, nil
	}
	return &authfeature.AuthPluginClaims{
		Subject:       c.Subject,
		Email:         c.Email,
		EmailVerified: c.EmailVerified,
		Name:          c.Name,
		Roles:         c.Roles,
		Scopes:        c.Scopes,
		Provider:      c.Provider,
		Raw:           c.Raw,
	}, nil
}

func (a *authPluginAdapter) Logout(ctx context.Context, subject string) error {
	return a.inner.Logout(ctx, subject)
}

// resolveAndRegisterAuthPlugins resolves Type=plugin auth configs to
// the corresponding kind=auth plugin clients, wraps them in the local
// adapter, and installs them into authfeature via RegisterAuthPlugins.
func (s *Server) resolveAndRegisterAuthPlugins() error {
	if s.Config == nil || len(s.Config.Auth) == 0 {
		return nil
	}
	reg := plugins.Default()
	loaded := map[string]kinds.AuthPlugin{}
	if reg != nil {
		loaded = kinds.LoadAuth(reg)
	}
	out := map[string]authfeature.AuthPlugin{}
	for name, cfg := range s.Config.Auth {
		if cfg == nil || !strings.EqualFold(cfg.Type, "plugin") {
			continue
		}
		p, ok := loaded[cfg.Plugin]
		if !ok {
			return fmt.Errorf("auth %q: plugin %q not found (or wrong kind)", name, cfg.Plugin)
		}
		out[name] = &authPluginAdapter{inner: p}
	}
	authfeature.RegisterAuthPlugins(out)
	return nil
}

// validateAuthRefs walks every Auth entry and confirms that any
// `type: plugin` config refers to a registered kind=auth plugin. Fails
// the boot so we never serve an unauth-enforced 500 at request time.
func (s *Server) validateAuthRefs() error {
	if s.Config == nil || len(s.Config.Auth) == 0 {
		return nil
	}
	reg := plugins.Default()
	authNames := map[string]struct{}{}
	if reg != nil {
		for _, n := range reg.NamesOfKind(plugins.KindAuth) {
			authNames[n] = struct{}{}
		}
	}
	for name, cfg := range s.Config.Auth {
		if cfg == nil || !strings.EqualFold(cfg.Type, "plugin") {
			continue
		}
		if cfg.Plugin == "" {
			return fmt.Errorf("auth %q: type=plugin requires a `plugin:` field", name)
		}
		if _, ok := authNames[cfg.Plugin]; !ok {
			return fmt.Errorf(
				"auth %q: plugin %q not found (or wrong kind)",
				name, cfg.Plugin,
			)
		}
	}
	return nil
}
