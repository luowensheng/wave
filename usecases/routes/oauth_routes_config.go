package routes

import oauthrt "github.com/luowensheng/wave/usecases/oauth_routes"

// OAuthStartConfig — GET /login/<provider>; redirects to the IdP's consent screen.
type OAuthStartConfig = oauthrt.StartConfig

// OAuthCallbackConfig — GET /login/<provider>/callback; handles ?code=...&state=...
type OAuthCallbackConfig = oauthrt.CallbackConfig
