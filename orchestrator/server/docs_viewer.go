package servers

import "net/http"

// registerDocsViewer mounts a tiny browser-side viewer for the OpenAPI
// spec at /docs. Pure HTML + ~50 lines of vanilla JS — no Swagger UI
// asset bundle, no CDN. Renders the path table grouped by tag with
// expandable details. Good enough for internal tools and CI smoke
// tests; reach for full Swagger UI if you need request execution UI.
func (s *Server) registerDocsViewer() {
	s.mux.HandleFunc("/docs", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(docsHTML))
	})
	s.mux.HandleFunc("/docs/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/docs", http.StatusFound)
	})
}

const docsHTML = `<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>wave — API docs</title>
<style>
  :root { color-scheme: light dark; }
  body { font: 14px/1.4 system-ui, -apple-system, sans-serif; max-width: 1100px; margin: 1em auto; padding: 0 1em; }
  h1 { font-size: 1.6em; margin: 0 0 0.4em; }
  h2 { font-size: 1.05em; margin: 1.6em 0 0.3em; border-bottom: 1px solid #8884; padding-bottom: 0.2em; text-transform: uppercase; letter-spacing: 0.05em; color: #666; }
  details { border: 1px solid #8882; border-radius: 4px; margin: 0.4em 0; padding: 0; background: #fff1; }
  details > summary { padding: 6px 10px; cursor: pointer; display: flex; gap: 12px; align-items: baseline; }
  summary::-webkit-details-marker { display: none; }
  .method { font-family: ui-monospace, monospace; font-weight: 600; min-width: 60px; padding: 2px 6px; border-radius: 3px; font-size: 12px; }
  .m-get   { background: #56b1ff44; color: #1c6fcb; }
  .m-post  { background: #4eb46644; color: #2e7a3a; }
  .m-put   { background: #d9883844; color: #a16021; }
  .m-patch { background: #d9883844; color: #a16021; }
  .m-delete{ background: #d3535344; color: #b03a3a; }
  .path { font-family: ui-monospace, monospace; flex: 1; }
  .summary { color: #888; font-size: 12px; }
  .pill { display: inline-block; padding: 1px 6px; border-radius: 8px; background: #8881; font-size: 11px; }
  pre { background: #8881; padding: 0.7em; border-radius: 3px; overflow-x: auto; font-size: 12px; margin: 0; }
  .ext-table { font-size: 12px; padding: 8px 12px; border-top: 1px solid #8882; background: #8881; }
  .ext-table b { color: #888; font-weight: 600; }
  .meta { font-size: 12px; color: #888; margin-bottom: 1em; }
  a, a:visited { color: #1c6fcb; }
</style>
</head>
<body>
<h1>API docs</h1>
<div class="meta">
  Spec: <a href="/openapi.json">/openapi.json</a> ·
  Health: <a href="/healthz">/healthz</a> · <a href="/readyz">/readyz</a> ·
  Metrics: <a href="/metrics">/metrics</a> ·
  Admin: <a href="/admin/">/admin/</a>
</div>
<div id="root">Loading…</div>

<script>
(async () => {
  const root = document.getElementById('root');
  let spec;
  try {
    spec = await fetch('/openapi.json').then(r => r.json());
  } catch (e) {
    root.textContent = 'Failed to load /openapi.json: ' + e;
    return;
  }
  const byTag = {};
  for (const [path, ops] of Object.entries(spec.paths || {})) {
    for (const [method, op] of Object.entries(ops)) {
      const tag = (op.tags && op.tags[0]) || 'untagged';
      (byTag[tag] = byTag[tag] || []).push({ path, method, op });
    }
  }
  const tags = Object.keys(byTag).sort();
  const html = [];
  html.push('<div class="meta">' + spec.info.title + ' ' + (spec.info.version || '') + '</div>');
  for (const tag of tags) {
    html.push('<h2>' + escape(tag) + '</h2>');
    for (const r of byTag[tag].sort((a,b) => (a.path+a.method).localeCompare(b.path+b.method))) {
      const methodCls = 'method m-' + r.method.toLowerCase();
      const sec = (r.op.security || []).map(s => Object.keys(s)[0]).join(', ');
      html.push('<details>');
      html.push('<summary>');
      html.push('<span class="' + methodCls + '">' + r.method.toUpperCase() + '</span>');
      html.push('<span class="path">' + escape(r.path) + '</span>');
      if (r.op.summary) html.push('<span class="summary">' + escape(r.op.summary) + '</span>');
      if (sec) html.push('<span class="pill">auth: ' + escape(sec) + '</span>');
      html.push('</summary>');
      const ext = {};
      for (const k of Object.keys(r.op)) if (k.startsWith('x-wave')) ext[k] = r.op[k];
      if (Object.keys(ext).length) {
        html.push('<div class="ext-table">');
        for (const [k,v] of Object.entries(ext)) {
          html.push('<div><b>' + escape(k) + ':</b> <code>' + escape(JSON.stringify(v)) + '</code></div>');
        }
        html.push('</div>');
      }
      html.push('<pre>' + escape(JSON.stringify(r.op, null, 2)) + '</pre>');
      html.push('</details>');
    }
  }
  root.innerHTML = html.join('');
  function escape(s) { return String(s).replace(/[&<>"']/g, c => ({'&':'&amp;','<':'&lt;','>':'&gt;','"':'&quot;',"'":'&#39;'}[c])); }
})();
</script>
</body>
</html>
`
