package oauth

// google is the OAuth-2 (non-OIDC) flavor for Google sign-in. Defaults
// the standard Google endpoints; otherwise identical to generic.
//
// For modern usage prefer infra/oidc with issuer
// "https://accounts.google.com" — that handles JWKS rotation
// correctly. This OAuth flavor exists for symmetry with the GitHub /
// Apple providers and for users who want raw access tokens (e.g. to
// call Gmail / Drive APIs).
type google struct{ *generic }

func newGoogle(c Config) (Provider, error) {
	if c.AuthorizeURL == "" {
		c.AuthorizeURL = "https://accounts.google.com/o/oauth2/v2/auth"
	}
	if c.TokenURL == "" {
		c.TokenURL = "https://oauth2.googleapis.com/token"
	}
	if c.UserinfoURL == "" {
		c.UserinfoURL = "https://www.googleapis.com/oauth2/v3/userinfo"
	}
	if len(c.Scopes) == 0 {
		c.Scopes = []string{"openid", "email", "profile"}
	}
	g, err := newGeneric(c)
	if err != nil {
		return nil, err
	}
	return &google{generic: g}, nil
}

func (g *google) Name() string { return "google_oauth" }
