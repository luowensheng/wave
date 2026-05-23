# Push notifications via Firebase Cloud Messaging

Send a push to iOS, Android, or a web browser. FCM is free, works
cross-platform, and exposes a simple HTTPS API. Wave's `type: fetch`
calls it.

::: tip
Firebase **Auth** (sign-in) is a separate product. For that, use
FCM's sibling Identity Toolkit API the same way — or just use
the [Google Sign-In recipe](/cookbook/google-signin) which is OIDC
under the hood.
:::

## What you need from Firebase first

1. Create a project at [console.firebase.google.com](https://console.firebase.google.com)
2. **Project Settings → Service Accounts → Generate new private key**
   — downloads a JSON file
3. **Cloud Messaging** → note the project ID
4. (Client side) integrate the Firebase SDK in your iOS / Android / web app
   to obtain device **registration tokens**

The service-account JSON gives Wave permission to send pushes.

## Approach 1 — Send via the legacy server key (simplest, deprecating)

```yaml
default:
  port: 8080

env:
  FCM_SERVER_KEY: { description: "Cloud Messaging → Server Key (legacy)" }

requests:
  fcm_send_legacy:
    method: POST
    url: https://fcm.googleapis.com/fcm/send
    headers:
      Authorization: "key=${env:FCM_SERVER_KEY}"
      Content-Type:  "application/json"
    body: |
      {
        "to": "{{token}}",
        "notification": {
          "title": "{{title}}",
          "body":  "{{body}}"
        },
        "data": {{toJSON .data}}
      }

routes:
  - path: /api/push
    method: POST
    auth: [app]
    type: fetch
    inputs:
      - { name: token, source: body, type: string, required: true }
      - { name: title, source: body, type: string, required: true, max: 200 }
      - { name: body,  source: body, type: string, required: true, max: 500 }
      - { name: data,  source: body, type: object, default: {} }
    fetch:
      request: fcm_send_legacy
      response:
        output_template: '{"sent": true, "message_id": "{{.Body.message_id}}"}'
```

The legacy `key=SERVER_KEY` auth is being deprecated by Google. The
modern path uses **OAuth 2.0 service-account tokens** — a tiny
plugin generates them.

## Approach 2 — Send via the HTTP v1 API (modern, recommended)

The HTTP v1 API needs an OAuth access token derived from the
service-account JSON. Wave can't do JWT signing in YAML, so a
20-line Go plugin handles it.

```yaml
env:
  FCM_PROJECT_ID:                { description: "Firebase project ID" }
  FCM_SERVICE_ACCOUNT_PATH:      { description: "Path to service-account.json" }

plugins:
  fcm:
    transport: longlived
    kind: handler
    command: ["/usr/local/bin/wave-fcm-push"]
    timeout: 10s
    env:
      FCM_PROJECT_ID:           "${env:FCM_PROJECT_ID}"
      FCM_SERVICE_ACCOUNT_PATH: "${env:FCM_SERVICE_ACCOUNT_PATH}"

storage:
  app:
    tables:
      device_tokens:
        columns:
          - id       INTEGER PRIMARY KEY AUTOINCREMENT
          - user_id  TEXT NOT NULL
          - token    TEXT NOT NULL UNIQUE
          - platform TEXT NOT NULL          -- ios | android | web
          - at       TEXT NOT NULL DEFAULT (datetime('now'))

routes:
  # 1. Client registers its device token on app launch
  - path: /api/push/register
    method: POST
    auth: [app]
    type: storage-access
    inputs:
      - { name: token,    source: body, type: string, required: true }
      - { name: platform, source: body, type: string, required: true, enum: [ios, android, web] }
    storage-access:
      source: app
      execute: |
        INSERT INTO device_tokens(user_id, token, platform)
        VALUES ({{getUser}}, {{token}}, {{platform}})
        ON CONFLICT(token) DO UPDATE SET user_id = excluded.user_id
      output_template: '{"registered": true}'

  # 2. Send a push to a specific user
  - path: /api/push/send
    method: POST
    auth: [app]
    require_roles: [admin]
    type: plugin
    inputs:
      - { name: user_id, source: body, type: string, required: true }
      - { name: title,   source: body, type: string, required: true, max: 200 }
      - { name: body,    source: body, type: string, required: true, max: 500 }
      - { name: data,    source: body, type: object, default: {} }
    plugin:
      name: fcm
      trigger_key: send_to_user
```

The plugin (Go, ~80 lines):

```go
// wave-fcm-push — long-lived FCM HTTP v1 sender
package main

import (
  "context"
  "encoding/json"
  "fmt"
  "os"
  "time"

  "golang.org/x/oauth2/google"
  "google.golang.org/api/option"
  fcm "google.golang.org/api/fcm/v1"
  sdk "github.com/luowensheng/wave/sdk/wave"
)

type sender struct {
  client    *fcm.Service
  projectID string
}

func (s *sender) Call(ctx context.Context, req *sdk.Request) (*sdk.Response, error) {
  var body struct {
    UserID string         `json:"user_id"`
    Title  string         `json:"title"`
    Body   string         `json:"body"`
    Data   map[string]any `json:"data"`
  }
  if err := json.Unmarshal(req.Body, &body); err != nil {
    return errResp(400, "bad request"), nil
  }

  // … fetch tokens for body.UserID from Wave's storage via a callback
  // (in practice, pass tokens in the request body to keep the plugin
  // stateless; or query the DB directly from the plugin)
  tokens := []string{ /* … */ }

  for _, token := range tokens {
    _, err := s.client.Projects.Messages.Send(
      fmt.Sprintf("projects/%s", s.projectID),
      &fcm.SendMessageRequest{Message: &fcm.Message{
        Token:        token,
        Notification: &fcm.Notification{Title: body.Title, Body: body.Body},
        Data:         stringifyMap(body.Data),
      }},
    ).Context(ctx).Do()
    if err != nil { /* log + skip */ }
  }
  out, _ := json.Marshal(map[string]any{"sent": len(tokens)})
  return &sdk.Response{Status: 200, Body: out}, nil
}

func (s *sender) Close() error { return nil }

func main() {
  ctx := context.Background()
  data, _ := os.ReadFile(os.Getenv("FCM_SERVICE_ACCOUNT_PATH"))
  creds, _ := google.CredentialsFromJSON(ctx, data, fcm.CloudPlatformScope)
  svc, _ := fcm.NewService(ctx, option.WithCredentials(creds))

  if err := sdk.RunHandler(&sender{client: svc, projectID: os.Getenv("FCM_PROJECT_ID")}); err != nil {
    fmt.Fprintln(os.Stderr, err); os.Exit(1)
  }
  _ = time.Time{}
}

func errResp(status int, msg string) *sdk.Response {
  b, _ := json.Marshal(map[string]string{"error": msg})
  return &sdk.Response{Status: status, Body: b}
}
func stringifyMap(m map[string]any) map[string]string {
  out := map[string]string{}
  for k, v := range m { out[k] = fmt.Sprint(v) }
  return out
}
```

Build: `go build -o /usr/local/bin/wave-fcm-push .`

::: tip Plugin language doesn't matter
This could just as well be Python — `firebase_admin` library does
the same thing in ~10 lines. See the [Build a plugin recipe](/cookbook/build-plugin)
for the Python framing scaffold.
:::

## Fan-out push (to a topic)

FCM lets you subscribe many devices to a "topic" and push to all of
them with one API call:

```yaml
requests:
  fcm_send_topic:
    method: POST
    url: "https://fcm.googleapis.com/v1/projects/${env:FCM_PROJECT_ID}/messages:send"
    headers:
      Authorization: "Bearer {{access_token}}"     # provided by the plugin
      Content-Type:  "application/json"
    body: |
      {
        "message": {
          "topic": "{{topic}}",
          "notification": {"title": "{{title}}", "body": "{{body}}"}
        }
      }
```

## Use the outbox for batches

For "send a notification to every user" or "weekly digest", queue
each send via the [outbox](/cookbook/outbox) instead of looping in
the handler. The outbox worker retries failures and you get the
DLQ + replay CLI for free.

## Production checklist

- [ ] **Rotate the service-account JSON** annually; keep it OUT of
      git (mount it from a secret manager via the [vault-secrets
      plugin](https://github.com/luowensheng/wave/tree/main/examples/plugins/vault-secrets))
- [ ] **Clean up dead tokens** — FCM returns `NotRegistered` /
      `InvalidRegistration` for stale device tokens. Delete the row
      from `device_tokens`.
- [ ] **Rate-limit** the registration endpoint — devices can spam it
      across rapid app restarts
- [ ] **Don't push from the request thread** for batches — use the
      outbox; otherwise a slow FCM response holds up your user
- [ ] **Test sending** in Firebase Console first (Cloud Messaging
      → Send test message) to confirm credentials work before
      wiring up your end

## See also

- [Send transactional email](/cookbook/send-email) — same `type: fetch` pattern over email
- [Outbox-backed delivery](/cookbook/outbox) — for batch pushes
- [Build a plugin](/cookbook/build-plugin) — for the OAuth-token-generating plugin
- [Google Sign-In](/cookbook/google-signin) — sister product (Firebase Auth uses same OIDC under the hood)
