# Auth

Wave ships with first-class support for the auth patterns most apps
need. Declare an auth backend under `auth:`, then attach `auth:
[name]` to any route that requires it.

## Built-in auth types

| `type` | What it does | Best for |
|---|---|---|
| `jwt` | Signs/validates HMAC-SHA256 JWT cookies | Simple username/password apps |
| `oidc` | OIDC discovery + ID-token validation | Google / Okta / Auth0 / Entra |
| `oauth2` | Plain OAuth 2.0 (no OIDC) | GitHub, Apple, custom |
| `saml` | SAML 2.0 SSO | Enterprise (via plugin) |
| `magic_link` | One-shot email token | Passwordless apps |
| `totp` | RFC 6238 TOTP secondary factor | 2FA on top of any other auth |
| `plugin` | Hand off to a custom worker | LDAP / proprietary IdPs |

## Pattern: protect a route

```yaml
auth:
  primary:
    type: jwt
    secret: "${env:JWT_SECRET}"
    cookie_name: session

routes:
  - path: /me
    method: GET
    type: storage-access
    auth: [primary]                    # 401 if no valid cookie
    storage-access:
      source: app
      execute: "SELECT id, name FROM users WHERE id = {{getUser}}"
      output_template: '{{toJSON .Data}}'
```

`auth:` is a **list** — a request must satisfy *all* listed
backends. Most routes name just one.

## Pattern: RBAC

```yaml
- path: /admin/users
  method: GET
  auth: [primary]
  require_roles: [admin]               # roles claim from ID token
  require_claims:                      # arbitrary claim match
    plan: enterprise
    email_verified: "true"
  type: storage-access
  storage-access: { ... }
```

`require_roles` reads the `roles` (or `groups`) claim from the
ID token; `require_claims` exact-matches arbitrary key/value pairs.
Both are AND-ed.

## Auth flow recipes

| Recipe | When to use |
|---|---|
| [Magic-link login](/cookbook/magic-link-login) | Most apps — passwordless is the modern default |
| [OAuth (Google/GitHub)](/cookbook/oauth) | "Sign in with X" |
| Username/password + JWT | Internal tools — see [`password-jwt`](https://github.com/luowensheng/wave/tree/main/examples/apps/password-jwt) |
| [TOTP 2FA](https://github.com/luowensheng/wave/tree/main/examples/apps/totp-2fa) | High-security accounts |
| [SAML SSO](https://github.com/luowensheng/wave/tree/main/examples/apps/saml-sso) | Enterprise (via plugin) |

## Forward auth (delegate to an external service)

```yaml
- path: /api/secret
  method: GET
  forward_auth:
    url: http://auth-svc:9000/verify
    method: GET
    timeout_sec: 2
    forward_headers: [Authorization, Cookie]
    response_headers: [X-User-Id]
  type: storage-access
  storage-access: { ... }
```

Per request, Wave calls the auth service with the listed headers.
On a 2xx, it copies response headers back onto the request and
proceeds; on non-2xx the route never runs and the client sees the
auth service's response.

Use this for centralized auth across multiple Wave instances or
language tiers.

## CSRF

```yaml
- path: /transfer
  method: POST
  type: storage-access
  validate_csrf: true                  # require valid CSRF token
  storage-access: { ... }

- path: /forms/transfer
  method: GET
  type: content
  include_csrf: true                   # set CSRF cookie + token
  content: { ... }
```

Tokens are HMAC-signed with the server secret. The `validate_csrf`
middleware checks the `X-CSRF-Token` header against the cookie.

## See also

- [Magic-link recipe](/cookbook/magic-link-login)
- [OAuth recipe](/cookbook/oauth)
- [Audit log recipe](/cookbook/audit-log) — pair with auth for
  `{{getUser}}`-aware logs
- [docs/auth-plugins.md](https://github.com/luowensheng/wave/blob/main/docs/auth-plugins.md)
