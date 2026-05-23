# Sign in with Google

Add "Sign in with Google" to your app in ~30 lines of YAML. Wave
handles the OAuth dance, ID-token verification, session cookie, and
RBAC by domain or email-verified claim.

## What you need from Google first

1. Go to [console.cloud.google.com](https://console.cloud.google.com/apis/credentials)
2. Create an OAuth 2.0 Client ID (type: **Web application**)
3. Authorized redirect URI: `http://localhost:8080/auth/google/callback`
   (add production URL too when you deploy)
4. Copy the Client ID + Client Secret

## YAML

```yaml
default:
  port: 8080

env:
  GOOGLE_CLIENT_ID:     { description: "OAuth client ID from console.cloud.google.com" }
  GOOGLE_CLIENT_SECRET: { description: "OAuth client secret" }

auth:
  google:
    type: oidc                               # OIDC = OAuth2 + ID token verification
    issuer: https://accounts.google.com
    client_id:     "${env:GOOGLE_CLIENT_ID}"
    client_secret: "${env:GOOGLE_CLIENT_SECRET}"
    scopes: [openid, email, profile]
    redirect_uri: http://localhost:8080/auth/google/callback
    cookie_name: session
    cookie_max_age_seconds: 604800           # 1 week
    cookie_secure: false                     # true in production (HTTPS)
    cookie_same_site: Lax

routes:
  # 1. Browser hits /login → Wave redirects to Google
  - path: /login
    method: GET
    type: oauth-start
    oauth-start:
      for: google

  # 2. Google redirects back here → Wave validates, sets cookie, redirects
  - path: /auth/google/callback
    method: GET
    type: oauth-callback
    oauth-callback:
      for: google
      redirect_on_success: /me
      redirect_on_failure: /login?err=oauth

  # 3. Protected route — only reachable with a valid session
  - path: /me
    method: GET
    auth: [google]                            # 401/302 if not signed in
    type: content
    content:
      status_code: 200
      headers: [["Content-Type", "application/json"]]
      body: '{"signed_in": true}'

  # 4. Sign-out
  - path: /logout
    method: POST
    auth: [google]
    type: auth-logout
    auth-logout:
      for: google
      redirect_on_success: /
```

## Try it

```sh
export GOOGLE_CLIENT_ID=...
export GOOGLE_CLIENT_SECRET=...
wave serve server.yaml --port 8080

# Open in a browser:
open http://localhost:8080/login
# → Google consent → land on /me with session cookie set
```

## Use the user's identity inside a route

Once `auth: [google]` is on a route, `{{getUser}}` returns the
authenticated user's subject (Google's stable user id). Useful for
scoping queries:

```yaml
- path: /api/notes
  method: GET
  auth: [google]
  type: storage-access
  storage-access:
    source: app
    execute: "SELECT * FROM notes WHERE user_id = {{getUser}} ORDER BY id DESC"
    output_template: '{{toJSON .Data}}'
```

## Gate on Google Workspace domain

For internal tools — only let `@yourcompany.com` accounts sign in:

```yaml
- path: /admin
  method: GET
  auth: [google]
  require_claims:
    hd: yourcompany.com            # Google's "hosted domain" claim
    email_verified: "true"
  type: content
  content: { body: "admin view" }
```

A signed-in user without `hd: yourcompany.com` gets 403.

## Gate on a Google Group

Google Workspace doesn't ship group memberships in the ID token by
default. Two options:
- **Use a custom claim**: configure Cloud Identity to add a group
  claim to the ID token, then check `require_claims: {groups: ...}`.
- **Use Google Workspace Directory API**: query the API at sign-in
  via a Go plugin; the plugin returns the user's groups as `roles`.
  Then `require_roles: [admins]` works.

The Directory-API pattern is in [`examples/apps/oidc-okta`](https://github.com/luowensheng/wave/tree/main/examples/apps/oidc-okta) (same shape for Google).

## Production checklist

- [ ] Add the **production redirect URI** in Google Console
- [ ] Set `cookie_secure: true` (you're on HTTPS in prod)
- [ ] Set `cookie_same_site: Lax` (or `None` if your frontend is on a different origin and you set `cookie_secure: true`)
- [ ] Rate-limit `/login` to prevent enumeration attacks
- [ ] Pair with **[Audit log](/cookbook/audit-log)** to record every sign-in
- [ ] Add **[TOTP 2FA](https://github.com/luowensheng/wave/tree/main/examples/apps/totp-2fa)** for high-value accounts

## See also

- [OAuth generic recipe](/cookbook/oauth) — GitHub, Apple, custom providers
- [Auth concept](/guide/concepts-auth) — all auth types Wave supports
- Demos: [`oauth-google`](https://github.com/luowensheng/wave/tree/main/examples/apps/oauth-google), [`oidc-okta`](https://github.com/luowensheng/wave/tree/main/examples/apps/oidc-okta)
