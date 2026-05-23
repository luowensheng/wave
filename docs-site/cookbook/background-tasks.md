# Background tasks

A request kicks off work that's too long for a synchronous reply.
The client gets a `task_id` immediately (202 Accepted) and watches
progress via SSE.

## How it works

```
1. POST /process       → 202 + {"task_id": "abc"}
2. Wave runs the plugin in a goroutine, emits each result to an SSE broker
3. Optional: each emission is also persisted via store:
4. Client opens GET /events/stream and watches for events
```

Three pieces: a plugin that does the work, an SSE connection to
publish to, and a `type: task` route that wires them.

## YAML

```yaml
default:
  port: 8080

plugins:
  llm:
    kind: subprocess
    command: ["python3", "worker.py"]

connections:
  events:
    type: sse
    subscribe_path: /events/stream
    buffer_size: 256

storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      results:
        columns:
          - id      INTEGER PRIMARY KEY AUTOINCREMENT
          - content TEXT
          - at      TEXT NOT NULL DEFAULT (datetime('now'))

routes:
  - path: /process
    method: POST
    type: task
    inputs:
      - { name: prompt, source: body, type: string, required: true }
    task:
      plugin: llm
      trigger_key: chat
      streaming: true              # plugin emits ndjson lines
      connection: events
      event_type: result
      store:                       # persist each emitted event
        source: app
        inputs:
          content: content
        execute: "INSERT INTO results(content) VALUES ({{content}})"
```

## Try it

```sh
wave serve server.yaml --port 8080

# Open the SSE stream in one terminal
curl -N http://localhost:8080/events/stream

# In another terminal, kick off a task
curl -X POST -d '{"prompt":"hello"}' http://localhost:8080/process
# {"task_id":"a1b2c3d4..."}

# The SSE terminal prints each emitted line as it arrives.
```

## The plugin contract

A subprocess plugin reads JSON from stdin, writes JSON or ndjson to
stdout. The full contract is at
[docs/plugins.md](https://github.com/luowensheng/wave/blob/main/docs/plugins.md).
Minimal Python:

```python
import sys, json, time

req = json.loads(sys.stdin.read())
prompt = json.loads(req["body"])["prompt"]

# streaming: each line is one event
for i in range(5):
    print(json.dumps({"content": f"chunk {i} of '{prompt}'"}))
    sys.stdout.flush()
    time.sleep(0.5)
```

## Variations

- **No persistence**: drop the `store:` block — events publish to
  SSE only.
- **Non-streaming**: `streaming: false` → the whole stdout payload
  is one event.
- **Multiple workers**: declare each in `plugins:`, point different
  routes at each.
- **Durable**: combine with the [outbox](/cookbook/outbox) so the
  client gets the result even if it disconnects.

## See also

- Demos: [`background-task-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/background-task-demo),
  [`queue-worker-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/queue-worker-demo),
  [`streaming-file-processor`](https://github.com/luowensheng/wave/tree/main/examples/apps/streaming-file-processor)
- [Stream events with SSE](/cookbook/sse)
- [Schedule a cron job](/cookbook/schedule)
