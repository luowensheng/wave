# saml-auth — reference KindAuth plugin

A SAML 2.0 Service Provider exposed through the wave auth-plugin
contract. The plugin only translates SAML <-> Claims; sessions, cookies
and JWTs stay in the orchestrator.

## Build

```sh
go build -o wave-saml .
```

## Configure

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

## Methods

The plugin handles two `AuthRequest.Method` values:

- `saml_init` — builds an AuthnRequest; returns `Redirect` to the IdP.
- `saml_callback` — validates the SAMLResponse from
  `Credentials["SAMLResponse"]`; returns Claims.

Anything else returns `unsupported method`.

## Tests

Unit tests run with `go test ./...` and only exercise smoke paths
(method dispatch, logout no-op, refresh-uncached). Real SAML round
trips need an IdP; gate any such tests with the `samlintegration`
build tag.
