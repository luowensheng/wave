# File uploads & downloads

Accept multipart uploads, persist the binary to disk, save metadata
in storage, and serve the file back on a separate route.

## YAML

```yaml
default:
  port: 8080

storage:
  app:
    type: sqlite
    path: ./data.db
    tables:
      files:
        columns:
          - id       INTEGER PRIMARY KEY AUTOINCREMENT
          - filename TEXT NOT NULL
          - mime     TEXT NOT NULL
          - size     INTEGER NOT NULL
          - at       TEXT NOT NULL DEFAULT (datetime('now'))

routes:
  # Multipart upload
  - path: /files
    method: POST
    type: storage-access
    expected_content_type: multipart/form-data
    inputs:
      - { name: upload, source: body, type: file, required: true }
    storage-access:
      source: app
      execute: |
        INSERT INTO files(filename, mime, size)
        VALUES ({{upload.Filename}}, {{upload.ContentType}}, {{upload.Size}})
      response_content_type: application/json
      output_template: '{"id": {{.LastInsertID}}, "filename": "{{.upload.Filename}}"}'

  # Listing
  - path: /files
    method: GET
    type: storage-access
    storage-access:
      source: app
      execute: "SELECT id, filename, mime, size FROM files ORDER BY id DESC"
      output_template: '{{toJSON .Data}}'

  # Download by id — returns the binary with the original MIME
  - path: /files/{id}
    method: GET
    type: storage-access
    inputs:
      - { name: id, source: path, type: int, required: true }
    storage-access:
      source: app
      execute: "SELECT filename, mime, blob FROM files WHERE id = {{id}} LIMIT 1"
      if_empty_status: 404
      response_content_type: $filetype     # special sentinel: stream as file
```

## Try it

```sh
wave serve server.yaml --port 8080

# Upload
curl -F upload=@photo.jpg http://localhost:8080/files
# {"id": 1, "filename": "photo.jpg"}

# List
curl http://localhost:8080/files
# [{"id":1,"filename":"photo.jpg","mime":"image/jpeg","size":98432}]

# Download (Content-Type set from MIME, body is the raw binary)
curl -O http://localhost:8080/files/1
```

## What's automatic

- **Multipart parsing**: `type: file` + `expected_content_type:
  multipart/form-data` tells the inputs middleware to expect a
  multipart form. The value bound to `upload` is a `*inputs.File`
  with `Filename`, `ContentType`, `Size`, and the raw content.
- **Body-size limit**: paired with `limits[case=body_too_large]` for
  consistent 413 handling. See [Rate-limit](/cookbook/rate-limit).
- **`$filetype` response**: special sentinel that tells the
  storage-access handler "this row is a binary file — stream it
  with the MIME from the row instead of formatting as JSON."

## Variations

- **Store on disk, not in the DB**: use a plugin or a
  `type: file-server` route pointing at the upload directory.
- **Pre-signed S3 URLs**: a `type: api` route that returns a
  signed URL, then the client uploads directly to S3. Wave just
  generates the URL.
- **Photo gallery with thumbnails**: see
  [`photo-gallery`](https://github.com/luowensheng/wave/tree/main/examples/apps/photo-gallery).

## Caveats

- **Default body limit**: the default per-route body limit is in the
  10 MB range. For larger files, raise it via `limits:`:

  ```yaml
  limits:
    big_upload: { case: body_too_large, max_bytes: 104857600 }   # 100 MB
  ```

  ```yaml
  routes:
    - path: /files
      method: POST
      limits: [big_upload]
      ...
  ```

- **SQLite has a row-size cap** around 1 GB. For larger files, use
  disk storage and only persist metadata.

## See also

- Demos: [`file-uploads`](https://github.com/luowensheng/wave/tree/main/examples/apps/file-uploads),
  [`photo-gallery`](https://github.com/luowensheng/wave/tree/main/examples/apps/photo-gallery)
- Concepts: [Inputs](/guide/concepts-inputs) — the `type: file` source
