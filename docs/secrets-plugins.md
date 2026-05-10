# Secrets plugins

Secrets-kind plugins extend `${PLUGIN:name:uri}` markers in the config.
Resolution happens in **two phases** because plugins themselves need
the config to start.

## Two-phase resolution

1. **Pre-parse pass** — `secrets.Expand` runs over the raw YAML bytes.
   `${ENV:...}` and `${FILE:...}` resolve here. `${PLUGIN:...}` markers
   pass through untouched (the unknown-prefix branch leaves them
   verbatim) — at this point no plugin process has been started.
2. **Post-boot pass** — after the plugin registry is built and the
   secrets-kind plugins are running, the orchestrator walks the parsed
   `Config` struct and substitutes every remaining `${PLUGIN:...}`
   marker in place. Unresolved markers (typo in plugin name, plugin
   not configured) become a boot-time error.

The practical implication: `${PLUGIN:...}` works in any string field
of the config (auth secrets, route inputs, storage DSNs, connection
URLs, …) but **not** in the `command:` line of a plugin block — those
need to be resolvable before plugins boot. Use `${ENV:...}` or
`${FILE:...}` there instead.

## Marker syntax

```
${PLUGIN:<plugin_name>:<uri>}
```

The first colon after `PLUGIN:` separates the plugin name from the
URI. The URI itself may contain colons, slashes, hashes, etc. — the
plugin defines its own URI grammar. Always check the plugin's README
for the expected format.

## Example: Vault

See `examples/plugins/vault-secrets/`. Config:

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
    type: jwt
    secret: ${PLUGIN:vault:secret/data/api#jwt_secret}
```

## Writing a secrets plugin

Implement `sdk.SecretsPlugin` and register with `sdk.RunSecrets`. The
`Resolve(ctx, uri)` method is the entire surface. Cache aggressively;
the orchestrator may call you many times on the same URI during boot.
