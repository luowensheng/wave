# OpenAI / Claude chat endpoint (streaming)

Expose a chat endpoint backed by OpenAI, Anthropic Claude, or any
OpenAI-compatible API (Groq, Together, Ollama, vLLM). Wave's
`type: task` route handles the 202-Accepted + SSE-progress pattern;
a thin plugin streams tokens from the model to your SSE broker.

::: tip
This is the *streaming* pattern. For one-shot non-streaming calls,
use `type: api` with `requests:` (see [send-email recipe](/cookbook/send-email)
for that shape).
:::

## Architecture

```
Browser  POST /api/chat → Wave returns 202 + {task_id}
                ↓
Wave kicks off the plugin in a goroutine
                ↓
Plugin calls OpenAI/Claude streaming API
                ↓
For each token: Plugin writes ndjson to stdout
                ↓
Wave reads each line → publishes as SSE event to /events/chat
                ↓
Browser EventSource receives each token in real time
```

## YAML

```yaml
default:
  port: 8080

env:
  OPENAI_API_KEY:    { description: "sk-... (or ANTHROPIC_API_KEY etc.)" }

plugins:
  llm:
    transport: longlived             # keep the process warm; saves spawn cost
    kind: handler
    command: ["python3", "llm_worker.py"]
    timeout: 60s
    env:
      OPENAI_API_KEY: "${env:OPENAI_API_KEY}"

connections:
  chat:
    type: sse
    subscribe_path: /events/chat
    buffer_size: 256
    keep_alive_interval: 15s

routes:
  # Public chat page
  - path: /
    method: GET
    type: content
    content:
      status_code: 200
      headers: [["Content-Type", "text/html; charset=utf-8"]]
      body: |
        <!doctype html>
        <h1>Chat</h1>
        <form id=f><input id=q size=60 placeholder="Ask anything"></form>
        <pre id=out></pre>
        <script>
          const es = new EventSource('/events/chat')
          es.addEventListener('token', e => out.textContent += JSON.parse(e.data).token)
          es.addEventListener('done',  e => out.textContent += '\n---\n')
          f.onsubmit = async e => {
            e.preventDefault()
            await fetch('/api/chat', {
              method: 'POST',
              headers: {'Content-Type': 'application/json'},
              body: JSON.stringify({prompt: q.value})
            })
            q.value = ''
          }
        </script>

  # Kick off a chat completion (returns 202 immediately)
  - path: /api/chat
    method: POST
    type: task
    inputs:
      - { name: prompt, source: body, type: string, required: true, min: 1, max: 8000 }
    task:
      plugin: llm
      trigger_key: chat
      streaming: true                # plugin emits one JSON line per token
      connection: chat
      event_type: token              # SSE `event:` field per chunk
```

## The plugin

::: code-group

```python [Python — OpenAI]
# llm_worker.py — long-lived plugin for OpenAI streaming
import sys, json, os
from openai import OpenAI

client = OpenAI(api_key=os.environ['OPENAI_API_KEY'])

def read_frame():
    headers = b''
    while not headers.endswith(b'\r\n\r\n'):
        b = sys.stdin.buffer.read(1)
        if not b: return None
        headers += b
    length = int(headers.split(b'Content-Length:')[1].strip().split(b'\r\n')[0])
    return json.loads(sys.stdin.buffer.read(length))

def write_frame(obj):
    body = json.dumps(obj).encode()
    sys.stdout.buffer.write(b'Content-Length: %d\r\n\r\n%s' % (len(body), body))
    sys.stdout.buffer.flush()

while True:
    req = read_frame()
    if req is None: break
    body = json.loads(req['params']['body'] or b'{}')
    prompt = body['prompt']

    # Streaming completion → emit each token as one JSON line
    stream = client.chat.completions.create(
        model='gpt-4o-mini',
        messages=[{'role':'user','content':prompt}],
        stream=True,
    )
    for chunk in stream:
        token = chunk.choices[0].delta.content or ''
        if token:
            # Each line becomes one SSE event (Wave emits per line in streaming: true mode)
            sys.stdout.write(json.dumps({'token': token}) + '\n')
            sys.stdout.flush()
    sys.stdout.write(json.dumps({'done': True}) + '\n')
    sys.stdout.flush()

    write_frame({
        'jsonrpc': '2.0',
        'id': req['id'],
        'result': {'status': 200, 'body': {'ok': True}},
    })
```

```python [Python — Anthropic Claude]
# Same shape, different SDK call
from anthropic import Anthropic
client = Anthropic(api_key=os.environ['ANTHROPIC_API_KEY'])

with client.messages.stream(
    model='claude-sonnet-4-5',
    max_tokens=4096,
    messages=[{'role':'user','content':prompt}],
) as stream:
    for token in stream.text_stream:
        sys.stdout.write(json.dumps({'token': token}) + '\n')
        sys.stdout.flush()
```

```js [Node.js — OpenAI]
// llm_worker.js
import OpenAI from 'openai'
import { createInterface } from 'readline'
const client = new OpenAI()

// (… JSON-RPC framing scaffold here, same shape as Python …)

async function handle(prompt) {
  const stream = await client.chat.completions.create({
    model: 'gpt-4o-mini',
    messages: [{role:'user',content:prompt}],
    stream: true,
  })
  for await (const chunk of stream) {
    const tok = chunk.choices[0]?.delta?.content || ''
    if (tok) process.stdout.write(JSON.stringify({token: tok}) + '\n')
  }
  process.stdout.write(JSON.stringify({done: true}) + '\n')
}
```

:::

::: tip Building a plugin in another language?
See the [Build a plugin recipe](/cookbook/build-plugin) for the
full framing spec and 5-language worked examples.
:::

## Try it

```sh
pip install openai     # or anthropic, or both
export OPENAI_API_KEY=sk-...
wave serve server.yaml --port 8080
open http://localhost:8080/
```

Type into the box; tokens stream into `<pre>` as they arrive.

## Local-only (Ollama, vLLM, LM Studio)

Same plugin shape — point the OpenAI SDK at a local URL:

```python
client = OpenAI(
    api_key='ollama',
    base_url='http://localhost:11434/v1',   # Ollama OpenAI-compatible endpoint
)
```

No code changes elsewhere. Use it for offline dev, private data, or
cost control.

## Add chat history (multi-turn)

Wave doesn't track conversation state for you — but a `messages`
table does:

```yaml
storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      messages:
        columns:
          - id      INTEGER PRIMARY KEY AUTOINCREMENT
          - user_id TEXT NOT NULL
          - role    TEXT NOT NULL          -- user | assistant
          - content TEXT NOT NULL
          - at      TEXT NOT NULL DEFAULT (datetime('now'))

routes:
  # Persist the user prompt + look up recent history in one pipeline,
  # then kick the task with the full history as input.
  - path: /api/chat
    method: POST
    auth: [app]
    type: task
    inputs:
      - { name: prompt, source: body, type: string, required: true, max: 8000 }
    task:
      plugin: llm
      trigger_key: chat
      streaming: true
      connection: chat
      event_type: token
      pre_query:
        source: app
        execute: |
          INSERT INTO messages(user_id, role, content) VALUES ({{getUser}}, 'user', {{prompt}});
          SELECT role, content FROM messages
            WHERE user_id = {{getUser}}
            ORDER BY id DESC LIMIT 20
      store:
        source: app
        # Persist each emitted token chunk so we have the assistant turn for next time.
        execute: |
          INSERT INTO messages(user_id, role, content) VALUES ({{getUser}}, 'assistant', {{token}})
```

## Production checklist

- [ ] **Auth + rate-limit** the chat route. Anonymous LLM endpoints
      get abused within hours. See [Rate-limit recipe](/cookbook/rate-limit) — bucket per `sub` claim.
- [ ] **Token budget per user** — track tokens used in a `usage`
      table, reject above quota.
- [ ] **Audit every prompt** — at minimum, log `user_id, prompt_hash,
      model, tokens, cost`. See [Audit log recipe](/cookbook/audit-log).
- [ ] **Content filtering** — wrap the plugin to check prompt/response
      against a moderation API before/after the call.
- [ ] **Streaming AND non-streaming** — some clients can't handle
      SSE. Offer both routes; non-streaming uses `type: api` with the
      regular `/v1/chat/completions` endpoint.
- [ ] **Timeouts** — set `plugins.llm.timeout` to your model's worst
      case (60-120s typical).

## See also

- [Plugins concept](/guide/concepts-plugins) — out-of-process workers
- [Build a plugin recipe](/cookbook/build-plugin) — same plugin in 5 languages
- [Background tasks](/cookbook/background-tasks) — `type: task` pattern in depth
- [Stream events with SSE](/cookbook/sse) — the SSE broker primitive
- Demos: [`background-task-demo`](https://github.com/luowensheng/wave/tree/main/examples/apps/background-task-demo), [`streaming-file-processor`](https://github.com/luowensheng/wave/tree/main/examples/apps/streaming-file-processor)
