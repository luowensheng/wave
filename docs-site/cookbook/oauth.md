# OAuth with Google / GitHub / Apple

Sign-in-with-X over OAuth 2.0 / OIDC. Wave handles the redirect dance,
PKCE, token exchange, ID-token verification, and session cookie.

## How the flow works

```
1. GET /auth/google           → 302 redirect to Google with PKCE state
2. Google → user consent → callback to /auth/google/callback
3. Wave verifies the ID token, sets a session cookie, redirects to /me
```

Two routes per provider: `oauth-start` (redirect to provider) and
`oauth-callback` (handle the return).

## YAML — Google

```yaml
default:
  port: 8080

env:
  GOOGLE_CLIENT_ID:     { description: "OAuth client ID" }
  GOOGLE_CLIENT_SECRET: { description: "OAuth client secret" }

auth:
  google:
    type: oidc
    issuer: https://accounts.google.com
    client_id: "${env:GOOGLE_CLIENT_ID}"
    client_secret: "${env:GOOGLE_CLIENT_SECRET}"
    scopes: [openid, email, profile]
    redirect_uri: http://localhost:8080/auth/google/callback
    cookie_name: session
    cookie_max_age_seconds: 86400

routes:
  - path: /auth/google
    method: GET
    type: oauth-start
    oauth-start: { for: google }

  - path: /auth/google/callback
    method: GET
    type: oauth-callback
    oauth-callback:
      for: google
      redirect_on_success: /me
      redirect_on_failure: /login?err=oauth

  - path: /me
    method: GET
    type: content
    auth: [google]
    content:
      status_code: 200
      headers: [["Content-Type", "application/json"]]
      body: |
        {"hello":"world"}
```

## Try it

```sh
# 1. Register the OAuth client at https://console.cloud.google.com
#    Authorized redirect URI: http://localhost:8080/auth/google/callback
# 2. Export the credentials:
export GOOGLE_CLIENT_ID=...
export GOOGLE_CLIENT_SECRET=...

# 3. Run
wave serve server.yaml --port 8080

# 4. Visit http://localhost:8080/auth/google in a browser
#    → Google consent → land on /me with session cookie set
```

## GitHub

Same shape, different `auth:` block:

```yaml
auth:
  github:
    type: oauth2
    auth_url:  https://github.com/login/oauth/authorize
    token_url: https://github.com/login/oauth/access_token
    user_url:  https://api.github.com/user
    client_id: "${env:GH_CLIENT_ID}"
    client_secret: "${env:GH_CLIENT_SECRET}"
    scopes: [read:user, user:email]
    redirect_uri: http://localhost:8080/auth/github/callback
```

## RBAC by provider claim

Once signed in, gate routes by claim:

```yaml
- path: /admin/users
  type: storage-access
  auth: [google]
  require_claims:
    email_verified: "true"
    hd: "example.com"             # Google Workspace domain only
  storage-access: { ... }
```

`require_roles` works against the `roles` / `groups` claim in the
ID token — useful for OIDC providers with role-based access.

## Variations

- **OIDC against Okta/Auth0/Entra**: use `type: oidc` and point
  `issuer:` at the provider's discovery URL.
- **SAML for enterprise SSO**: see
  [`examples/apps/saml-sso`](https://github.com/luowensheng/wave/tree/main/examples/apps/saml-sso).
- **Multiple providers on the same site**: declare each in `auth:`,
  give each its own `oauth-start` / `oauth-callback` route pair.
  Frontend offers a button per provider.
- **Apple Sign-In**: see
  [`examples/apps/oauth-apple`](https://github.com/luowensheng/wave/tree/main/examples/apps/oauth-apple) —
  uses an ES256 JWT as client_secret.

## See also

- Demos: [`oauth-google`](https://github.com/luowensheng/wave/tree/main/examples/apps/oauth-google),
  [`oauth-github`](https://github.com/luowensheng/wave/tree/main/examples/apps/oauth-github),
  [`oauth-apple`](https://github.com/luowensheng/wave/tree/main/examples/apps/oauth-apple),
  [`oidc-okta`](https://github.com/luowensheng/wave/tree/main/examples/apps/oidc-okta)
- Concepts: [Auth](/guide/concepts-auth)
