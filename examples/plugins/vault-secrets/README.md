# vault-secrets

Reference secrets-kind plugin: resolves secret references against a
HashiCorp Vault KV-v2 backend over plain HTTP. No Vault SDK required.

## URI format

```
${PLUGIN:vault:<kvpath>#<jsonkey>}
```

* `kvpath`  – Vault API path under `/v1/`, e.g. `secret/data/db`
* `jsonkey` – dotted path inside `data.data`, e.g. `password` or `creds.token`

## Environment

| Var               | Default | Notes                              |
| ----------------- | ------- | ---------------------------------- |
| `VAULT_ADDR`      | —       | required, e.g. `http://vault:8200` |
| `VAULT_TOKEN`     | —       | required, Vault auth token         |
| `VAULT_CACHE_TTL` | `5m`    | per-URI in-memory cache TTL        |

## Build and wire

```sh
go build -o /usr/local/bin/wave-vault .
```

```yaml
plugins:
  vault:
    kind: secrets
    transport: process
    command: ["/usr/local/bin/wave-vault"]
    env:
      VAULT_ADDR: ${ENV:VAULT_ADDR}
      VAULT_TOKEN: ${FILE:/run/secrets/vault_token}

auth:
  api:
    secret: ${PLUGIN:vault:secret/data/api#jwt_secret}
```
