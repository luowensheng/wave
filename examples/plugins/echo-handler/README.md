# echo-handler

Reference handler-kind plugin built on the wave SDK.

## Build

```sh
go build -o /tmp/echo-handler .
```

## Wire up

```yaml
plugins:
  echo:
    transport: process
    kind: handler           # optional; default
    command: /tmp/echo-handler

routes:
  - path: /echo
    type: plugin
    plugin: echo
```

## Smoke test

```sh
echo '{"jsonrpc":"2.0","id":1,"method":"handler.call","params":{"trigger_key":"x","body":"\"hi\""}}' | /tmp/echo-handler
```
