# Slack slash command

Build a `/foo` slash command for your Slack workspace. Wave handles
the signature verification, the synchronous response (within Slack's
3-second window), and optional follow-up messages via response_url.

## How Slack slash commands work

```
1. User types  /weather Boston       in a Slack channel
2. Slack POSTs form-encoded to your endpoint:
     command=/weather&text=Boston&user_id=U…&response_url=…
3. You have 3 seconds to return 200 with text (shown immediately)
4. For slower work, return 200 with empty text, then POST follow-up
   messages to the response_url asynchronously
```

## What you need from Slack first

1. Create an app at [api.slack.com/apps](https://api.slack.com/apps)
2. **Slash Commands** → New Command → set Request URL to
   `https://your-domain.com/slack/commands` (use `ngrok` or `cloudflared`
   in dev)
3. **Basic Information** → copy the **Signing Secret**

## YAML — synchronous response (fits in 3 seconds)

```yaml
default:
  port: 8080

env:
  SLACK_SIGNING_SECRET: { description: "From api.slack.com/apps → Basic Information" }

routes:
  - path: /slack/commands
    method: POST
    type: content
    expected_content_type: application/x-www-form-urlencoded
    webhook_sig:
      provider: slack
      secret: "${env:SLACK_SIGNING_SECRET}"
      tolerance_sec: 300
    inputs:
      - { name: command, source: body, type: string, required: true }
      - { name: text,    source: body, type: string, default: "" }
      - { name: user_id, source: body, type: string, required: true }
    content:
      status_code: 200
      headers: [["Content-Type", "application/json"]]
      body: |
        {
          "response_type": "in_channel",
          "text":          "Hello <@{{user_id}}>! You ran `{{command}} {{text}}`."
        }
```

`response_type`:
- `ephemeral` (default) — only the invoking user sees the reply
- `in_channel` — everyone in the channel sees it

## Try it

Dev setup (one-time):

```sh
brew install cloudflared
cloudflared tunnel --url http://localhost:8080   # gives you a public HTTPS URL
# Paste that URL + /slack/commands into the app's Request URL field
```

Run:

```sh
export SLACK_SIGNING_SECRET=...
wave serve server.yaml --port 8080
```

In Slack: type `/yourcommand hello world` — see the reply.

## Dispatch on subcommand

`/weather`, `/weather forecast Boston`, `/weather alert NYC` — route
them to different handlers using a `type: match` over the body:

```yaml
- path: /slack/commands
  method: POST
  type: match
  expected_content_type: application/x-www-form-urlencoded
  webhook_sig:
    provider: slack
    secret: "${env:SLACK_SIGNING_SECRET}"
  match:
    cases:
      - when: body
        match: { text: { prefix: "forecast" } }
        route:
          type: content
          content: { body: '{"text":"Weekly forecast: …"}' }

      - when: body
        match: { text: { prefix: "alert" } }
        route:
          type: storage-access
          storage-access:
            source: app
            execute: "INSERT INTO alerts(text) VALUES ({{text}})"
            output_template: '{"text":"Alert saved."}'

    default:
      route:
        type: content
        content: { body: '{"text":"Usage: /weather forecast <city> | alert <city>"}' }
```

## Long-running command (>3 sec): respond now, follow up later

Slack's 3-second deadline is strict. For anything slower (calling a
weather API, querying a DB, generating an image), return 200 immediately
with "working on it…" and POST the real answer to `response_url`.

```yaml
requests:
  slack_followup:
    method: POST
    url: "{{response_url}}"                       # response_url comes from the original webhook body
    headers:
      Content-Type: "application/json"
    body: |
      {
        "response_type": "in_channel",
        "replace_original": true,
        "text": "{{result}}"
      }

routes:
  - path: /slack/commands
    method: POST
    type: task                                    # 202 + background work
    expected_content_type: application/x-www-form-urlencoded
    webhook_sig:
      provider: slack
      secret: "${env:SLACK_SIGNING_SECRET}"
    inputs:
      - { name: text,         source: body, type: string }
      - { name: response_url, source: body, type: string, required: true }
    task:
      plugin: weather                             # your weather-fetching plugin
      trigger_key: lookup
      streaming: false
      # Send the immediate "working on it" reply via the route's normal response
      response_body: '{"text":"Looking up the weather…"}'
      # When the plugin finishes, POST the result to Slack's response_url
      then:
        - type: api
          ref: slack_followup
          inputs:
            response_url: response_url
            result:       result
```

## Interactive components (buttons, dropdowns)

Slack POSTs button clicks to a separate endpoint (the "Interactivity
& Shortcuts" URL). Same `webhook_sig:` validates them; the body is
JSON-wrapped in a `payload` form field.

```yaml
- path: /slack/interactive
  method: POST
  type: storage-access
  expected_content_type: application/x-www-form-urlencoded
  webhook_sig:
    provider: slack
    secret: "${env:SLACK_SIGNING_SECRET}"
  inputs:
    - { name: payload, source: body, type: object, required: true }   # JSON inside the form field
  storage-access:
    source: app
    execute: "INSERT INTO button_clicks(payload) VALUES ({{toJSON .payload}})"
    output_template: '{"text":"Got it."}'
```

## Production checklist

- [ ] **Slack's signature check is required** — never disable
      `webhook_sig`. Slack will refuse to install commands without it.
- [ ] **3-second deadline**: if your handler can take longer, switch to
      the `type: task` + `response_url` pattern shown above.
- [ ] **Rate-limit the endpoint** — a hostile workspace member can
      spam your command. 60/min/user is generous.
- [ ] **Persist commands** for audit; debug the inevitable "the bot
      did the wrong thing" reports.
- [ ] **Workspace install OAuth** for distributing to multiple
      workspaces — use the [OAuth recipe](/cookbook/oauth) with
      `auth_url: https://slack.com/oauth/v2/authorize`.

## See also

- [Stripe webhooks](/cookbook/stripe-webhooks) — same `webhook_sig:` pattern, different provider
- [Background tasks](/cookbook/background-tasks) — `type: task` deep dive
- [Match routes](/cookbook/multi-tenant) — `type: match` dispatch pattern (used in subcommand example above)
- Demo: [`slack-slash-command`](https://github.com/luowensheng/wave/tree/main/examples/apps/slack-slash-command)
