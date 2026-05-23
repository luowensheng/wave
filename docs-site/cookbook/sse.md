# Stream events with SSE

Push live updates to browsers without polling or WebSockets. Wave's
SSE brokers handle subscriber tracking, ring-buffer replay (so a
late-joining client still sees recent events), and proper headers.

## How it works

```
1. Browser opens an EventSource at /events/stream  (auto-registered)
2. Server-side code publishes payloads → broker.Publish(bytes)
3. All connected EventSource clients receive the event line.
```

You declare the broker in `connections:`. The subscribe path is
auto-registered as a route — don't add it manually.

## YAML

```yaml
default:
  port: 8080

connections:
  events:
    type: sse
    subscribe_path: /events/stream     # auto-registered as GET
    buffer_size: 256                    # ring buffer for reconnect replay

routes:
  # POST anything → published to the broker
  - path: /publish
    method: POST
    type: stream-publish
    inputs:
      - { name: msg, source: body, type: string, required: true }
    stream-publish:
      connection: events
      event_type: chat
      payload_template: '{"msg":"{{msg}}","at":"{{getCurrentTime}}"}'

  # Static frontend
  - path: /
    method: GET
    type: content
    content:
      status_code: 200
      headers: [["Content-Type", "text/html"]]
      body: |
        <!doctype html>
        <ul id=log></ul>
        <script>
          const es = new EventSource('/events/stream')
          es.addEventListener('chat', e => {
            const li = document.createElement('li')
            li.textContent = e.data
            document.getElementById('log').prepend(li)
          })
        </script>
```

## Try it

```sh
wave serve server.yaml --port 8080
```

Open `http://localhost:8080/` in two browser windows. From a third
terminal:

```sh
curl -X POST -d '{"msg":"hello"}' http://localhost:8080/publish
```

Both browsers receive the event instantly.

## SSE wire format

Each event arrives in this shape:

```
event: chat
data: {"msg":"hello","at":"2026-05-23T09:00:00Z"}

```

The blank line terminates the event. The browser's `EventSource`
parses it for you.

## Replay on reconnect

`buffer_size: 256` means the broker keeps the last 256 events. When
a client reconnects with a `Last-Event-ID` header, Wave replays
anything newer than that ID. No client-side bookkeeping required.

## Variations

- **Multiple named brokers** for fan-out:

  ```yaml
  connections:
    chat:    { type: sse, subscribe_path: /events/chat }
    alerts:  { type: sse, subscribe_path: /events/alerts }
    metrics: { type: sse, subscribe_path: /events/metrics }
  ```

  Each `stream-publish` route picks which broker to target.

- **Persist + publish**: combine with [Background tasks](/cookbook/background-tasks)
  to write each event to storage AND publish it.

- **Webhook → SSE bridge**: `type: stream-publish` with
  `webhook_sig:` accepts a Stripe/GitHub webhook and fans it out to
  browsers. See [`stripe-webhook-receiver`](https://github.com/luowensheng/wave/tree/main/examples/apps/stripe-webhook-receiver).

## See also

- Demos: [`sse-chat`](https://github.com/luowensheng/wave/tree/main/examples/apps/sse-chat),
  [`live-cursors`](https://github.com/luowensheng/wave/tree/main/examples/apps/live-cursors),
  [`event-fanout-hub`](https://github.com/luowensheng/wave/tree/main/examples/apps/event-fanout-hub)
- [Background tasks](/cookbook/background-tasks)
- [Forward Stripe webhooks](/cookbook/stripe-webhooks)
