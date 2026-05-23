# Image / file upload to S3 (or R2 / B2 / Spaces)

Don't upload large files through your Wave server. Generate a
**pre-signed PUT URL**, give it to the browser, let the browser
upload to S3 directly. Wave only persists metadata.

This works for **AWS S3**, **Cloudflare R2**, **Backblaze B2**,
**DigitalOcean Spaces**, **MinIO** — anything with an S3-compatible
API. Cloudflare R2 has the most generous free tier (10 GB free
forever, no egress fees).

## Architecture

```
1. Browser  POST /api/uploads  {filename, content_type}
                     ↓
2. Wave generates a presigned PUT URL via the S3-compat plugin
                     ↓
3. Wave returns {upload_url, key} to the browser
                     ↓
4. Browser PUT <file_bytes>  upload_url     ← skips Wave entirely
                     ↓
5. Browser POST /api/uploads/confirm  {key}
                     ↓
6. Wave persists the metadata row
```

Why this beats uploading through Wave:
- **No per-file body-limit bump** needed in Wave
- **No bandwidth cost** through your VM
- **CDN-cached** if you put one in front of the bucket
- **Works for any size** — Wave never touches the bytes

## Approach 1 — Pre-sign via a plugin

S3 pre-signing requires SigV4 — easier to do in a small Go plugin
than to template in YAML. The plugin returns the presigned URL.

```yaml
default:
  port: 8080

env:
  S3_ENDPOINT:    { description: "https://<account>.r2.cloudflarestorage.com or s3.amazonaws.com" }
  S3_BUCKET:      { description: "your-bucket-name" }
  S3_ACCESS_KEY:  { description: "Access key id" }
  S3_SECRET_KEY:  { description: "Secret access key" }

plugins:
  s3:
    transport: longlived
    kind: handler
    command: ["/usr/local/bin/wave-s3-presign"]
    timeout: 5s
    env:
      S3_ENDPOINT:   "${env:S3_ENDPOINT}"
      S3_BUCKET:     "${env:S3_BUCKET}"
      S3_ACCESS_KEY: "${env:S3_ACCESS_KEY}"
      S3_SECRET_KEY: "${env:S3_SECRET_KEY}"

storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      uploads:
        columns:
          - id           INTEGER PRIMARY KEY AUTOINCREMENT
          - user_id      TEXT NOT NULL
          - key          TEXT NOT NULL UNIQUE     -- S3 object key
          - filename     TEXT NOT NULL
          - content_type TEXT NOT NULL
          - size         INTEGER
          - at           TEXT NOT NULL DEFAULT (datetime('now'))

routes:
  # 1. Browser asks Wave for an upload URL
  - path: /api/uploads
    method: POST
    auth: [app]
    type: plugin
    inputs:
      - { name: filename,     source: body, type: string, required: true, max: 255 }
      - { name: content_type, source: body, type: string, required: true, max: 100 }
    plugin:
      name: s3
      trigger_key: presign_put
      # Plugin receives {filename, content_type, user_id} and returns {upload_url, key, expires_at}

  # 2. Browser confirms the upload finished; Wave persists the metadata
  - path: /api/uploads/confirm
    method: POST
    auth: [app]
    type: storage-access
    inputs:
      - { name: key,          source: body, type: string, required: true, max: 1024 }
      - { name: filename,     source: body, type: string, required: true, max: 255 }
      - { name: content_type, source: body, type: string, required: true, max: 100 }
      - { name: size,         source: body, type: int,    required: false }
    storage-access:
      source: app
      execute: |
        INSERT INTO uploads(user_id, key, filename, content_type, size)
        VALUES ({{getUser}}, {{key}}, {{filename}}, {{content_type}}, {{size}})
      output_template: '{"id": {{.LastInsertID}}}'

  # 3. List my uploads
  - path: /api/uploads
    method: GET
    auth: [app]
    type: storage-access
    storage-access:
      source: app
      execute: "SELECT id, key, filename, content_type, size, at FROM uploads WHERE user_id = {{getUser}} ORDER BY id DESC"
      output_template: '{{toJSON .Data}}'
```

The plugin (~50 lines of Go):

```go
// wave-s3-presign — long-lived handler plugin
package main

import (
  "context"
  "encoding/json"
  "fmt"
  "os"
  "time"

  "github.com/aws/aws-sdk-go-v2/aws"
  "github.com/aws/aws-sdk-go-v2/credentials"
  "github.com/aws/aws-sdk-go-v2/service/s3"
  sdk "github.com/luowensheng/wave/sdk/wave"
)

type presign struct {
  client *s3.PresignClient
  bucket string
}

func (p *presign) Call(ctx context.Context, req *sdk.Request) (*sdk.Response, error) {
  var body struct {
    Filename    string `json:"filename"`
    ContentType string `json:"content_type"`
  }
  if err := json.Unmarshal(req.Body, &body); err != nil {
    return &sdk.Response{Status: 400, Body: json.RawMessage(`{"error":"bad request"}`)}, nil
  }
  userID := req.Metadata["user_id"]      // populated by Wave from {{getUser}}

  key := fmt.Sprintf("u/%s/%d-%s", userID, time.Now().UnixNano(), body.Filename)
  out, err := p.client.PresignPutObject(ctx, &s3.PutObjectInput{
    Bucket:      aws.String(p.bucket),
    Key:         aws.String(key),
    ContentType: aws.String(body.ContentType),
  }, s3.WithPresignExpires(10*time.Minute))
  if err != nil {
    return &sdk.Response{Status: 500, Body: json.RawMessage(`{"error":"presign failed"}`)}, nil
  }
  resp, _ := json.Marshal(map[string]any{
    "upload_url": out.URL,
    "key":        key,
    "expires_at": time.Now().Add(10 * time.Minute).Format(time.RFC3339),
  })
  return &sdk.Response{Status: 200, Body: resp}, nil
}

func (p *presign) Close() error { return nil }

func main() {
  cfg := aws.Config{
    Region:      "auto",
    Credentials: credentials.NewStaticCredentialsProvider(
      os.Getenv("S3_ACCESS_KEY"), os.Getenv("S3_SECRET_KEY"), ""),
    BaseEndpoint: aws.String(os.Getenv("S3_ENDPOINT")),
  }
  client := s3.NewPresignClient(s3.NewFromConfig(cfg))
  if err := sdk.RunHandler(&presign{client: client, bucket: os.Getenv("S3_BUCKET")}); err != nil {
    os.Exit(1)
  }
}
```

Build: `go build -o /usr/local/bin/wave-s3-presign .`

::: tip
The same plugin works for AWS S3, Cloudflare R2, Backblaze B2, and
DigitalOcean Spaces — change `S3_ENDPOINT` and credentials.
:::

## Browser side

```js
// 1. Ask Wave for an upload URL
const r = await fetch('/api/uploads', {
  method: 'POST',
  credentials: 'include',
  headers: {'Content-Type': 'application/json'},
  body: JSON.stringify({
    filename:     file.name,
    content_type: file.type,
  }),
})
const { upload_url, key } = await r.json()

// 2. Upload directly to S3 — Wave is bypassed for the bytes
await fetch(upload_url, {
  method:  'PUT',
  body:    file,
  headers: {'Content-Type': file.type},
})

// 3. Confirm
await fetch('/api/uploads/confirm', {
  method: 'POST',
  credentials: 'include',
  headers: {'Content-Type': 'application/json'},
  body: JSON.stringify({
    key, filename: file.name, content_type: file.type, size: file.size,
  }),
})
```

## Approach 2 — Just proxy through Wave (small files only)

If you're uploading tiny files and want zero plugin code, Wave can
proxy. See [File uploads recipe](/cookbook/file-uploads).

## Approach 3 — `type: forward` to MinIO behind your firewall

If your storage is on your network, just `type: forward` to it with
auth in front:

```yaml
- path: /uploads/{key...}
  method: PUT
  type: forward
  auth: [app]
  forward:
    forward_url: "http://minio.internal:9000/${env:S3_BUCKET}/"
    include_headers:
      - ["Authorization", "AWS4-HMAC-SHA256 …"]
```

Simpler, no plugin — but the bytes go through Wave's VM.

## Production checklist

- [ ] **CORS on the bucket** — set the bucket's CORS policy to allow
      `PUT` from your frontend origin
- [ ] **Object-key prefixes per user** (`u/<userId>/...`) prevent
      one user from overwriting another's files
- [ ] **Max file size** enforced at presign time — clamp `Content-Length`
      in the policy
- [ ] **Virus scan** — for user-uploaded files, scan on confirm
      (ClamAV plugin, S3 Object Lambda, etc.)
- [ ] **Lifecycle rules** on the bucket — auto-delete unconfirmed
      uploads after 24 hours
- [ ] **Signed download URLs** for private buckets — same plugin,
      `presign_get` trigger_key
- [ ] **Image transforms** at the CDN edge (Cloudflare Images,
      Imgix, ImageKit) rather than in Wave

## See also

- [File uploads recipe](/cookbook/file-uploads) — simple proxy-through-Wave pattern
- [Build a plugin](/cookbook/build-plugin) — full plugin reference
- [Plugins concept](/guide/concepts-plugins)
