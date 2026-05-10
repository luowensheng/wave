# Auth plugins

`type: plugin` lets external processes implement authentication while
the orchestrator keeps owning sessions, cookies, JWTs, and redirects.
The pattern mirrors the OIDC and OAuth bridges: a typed adapter boots
once, and `AuthManager.Login` dispatches per-config based on `Type`.

## YAML

```yaml
plugins:
  saml_corp:
    kind: auth
    transport: process
    command: ./wave-saml
    env:
      SAML_IDP_METADATA_URL: "https://idp.corp/metadata"
      SAML_SP_ENTITY_ID:     "https://app.example.com/saml"
      SAML_SP_ACS_URL:       "https://app.example.com/saml/acs"
      SAML_SP_CERT_PATH:     "/etc/wave/sp.crt"
      SAML_SP_KEY_PATH:      "/etc/wave/sp.key"

auth:
  corp_sso:
    type: plugin
    plugin: saml_corp
    token_location: cookie
    cookie_name: corp_session
    redirect_on_success: /dashboard
    redirect_on_failure: /login
```

If `Type: plugin` and `plugin:` is missing, or names a plugin that is
not registered as `kind: auth`, the server fails fast at boot.

## What plugins do

A plugin implements `sdk.AuthPlugin`:

- `Authenticate(req)` — validate credentials / process a callback /
  build a redirect; return `Claims` (`Subject`, `Email`, `Roles`, ...).
- `RefreshClaims(subject)` — re-fetch user attributes / re-check group
  membership.
- `Logout(subject)` — best-effort revoke at the IdP.

The orchestrator does the rest: minting JWTs, building cookies, issuing
redirects, RBAC enforcement.

## Methods

`AuthRequest.Method` is plugin-defined. Conventional values:

- `password` — sent by built-in login forms with `username`/`password`
  in `Credentials`.
- `oauth_callback` — for OAuth-style flows where the plugin handles the
  IdP callback itself.
- `saml_init` / `saml_callback` — used by the reference SAML plugin to
  separate the AuthnRequest build from the assertion validation.

A plugin-aware route may set the `X-Auth-Method` request header to
override the default `password` method without changing the LoginForm
shape. Custom flow data flows through `Credentials` on the way in and
`SetCookies` / `Redirect` on the way out.

## Roles → RBAC

`Claims.Roles` is consumed by `infra/rbac.Middleware` exactly the way
OIDC roles are: any role match in the configured policy admits the
request. No plugin-side authorization is required.

## Cookies

`AuthResult.SetCookies` is appended verbatim to the orchestrator's
response, *in addition* to the standard auth cookie. Use it for
plugin-owned state (RelayState, nonces) that the plugin needs back on
the next request.

## Caveats

- The `Method` field is plugin-defined; coordinate the value set with
  the route handlers that call `Login`.
- `RefreshClaims` is currently called only on demand by callers; there
  is no background refresh loop.
- Plugin `Logout` is best-effort: a plugin error is logged but does not
  prevent the orchestrator from clearing the local session and cookie.
