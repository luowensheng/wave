package servers

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"wave/infra/audit"
	"wave/infra/connections"
	infrahttp "wave/infra/http"
	"wave/infra/metrics"
	"wave/infra/plugins"
)

// adminPageData is what the dashboard template renders.
type adminPageData struct {
	Hostname    string
	UptimeSec   int
	Version     string
	Routes      []adminRoute
	Connections []adminConn
	Plugins     []adminPlugin
	Metrics     string
}

type adminRoute struct {
	Method      string
	Path        string
	Type        string
	Auth        []string
	Description string
}

type adminConn struct {
	Name        string
	Type        string
	Path        string
	Subscribers int
	Published   int64
	Dropped     int64
}

type adminPlugin struct {
	Name      string
	Transport string
	Calls     int64
	Errors    int64
	AvgMs     float64
	Health    string // "ok" | "down" | "?"
	HealthErr string
}

// registerAdminDashboard installs GET /admin/ — a self-contained HTML
// status page (no JS dependencies). Read-only; safe to expose internally
// behind auth. Wrap it in your existing route auth by configuring an
// `admin` auth and adding it as a `Route` if you want extra hardening.
func (s *Server) registerAdminDashboard() {
	// GET-only registrations so a user's `GET /` file route doesn't
	// hit the Go 1.22 ServeMux "more methods than" conflict panic.
	s.mux.HandleFunc("GET /admin/", s.adminHandler)
	s.mux.HandleFunc("GET /admin", s.adminHandler)
}

func (s *Server) adminHandler(w http.ResponseWriter, r *http.Request) {
	audit.Emit(audit.Event{
		Action:    "admin.view",
		Outcome:   "success",
		IP:        infrahttp.ClientIP(r),
		UserAgent: r.UserAgent(),
		RequestID: infrahttp.RequestIDFrom(r.Context()),
		Target:    r.URL.Path,
	})
	host, _ := os.Hostname()
	data := adminPageData{
		Hostname:  host,
		UptimeSec: int(time.Since(bootTime).Seconds()),
		Version:   Version,
	}

	for _, route := range s.Config.Routes {
		data.Routes = append(data.Routes, adminRoute{
			Method:      defaultIfEmpty(strings.ToUpper(strings.TrimSpace(route.Method)), "GET"),
			Path:        route.Path,
			Type:        route.Type,
			Auth:        route.Auth,
			Description: route.Description,
		})
	}
	sort.Slice(data.Routes, func(i, j int) bool { return data.Routes[i].Path < data.Routes[j].Path })

	if reg := connections.Default(); reg != nil {
		for name, b := range reg.All() {
			subs, pub, drop := b.Stats()
			data.Connections = append(data.Connections, adminConn{
				Name:        name,
				Type:        defaultIfEmpty(b.Config().Type, "sse"),
				Path:        b.Config().SubscribePath,
				Subscribers: subs,
				Published:   pub,
				Dropped:     drop,
			})
		}
		sort.Slice(data.Connections, func(i, j int) bool { return data.Connections[i].Name < data.Connections[j].Name })
	}

	if preg := plugins.Default(); preg != nil {
		health := map[string]plugins.PluginHealth{}
		if hm := plugins.DefaultHealthMonitor(); hm != nil {
			for _, h := range hm.Snapshot() {
				health[h.Name] = h
			}
		}
		for _, ps := range preg.Stats() {
			t := ""
			if cfg, ok := s.Config.Plugins[ps.Name]; ok {
				t = cfg.Transport
			}
			row := adminPlugin{
				Name:      ps.Name,
				Transport: t,
				Calls:     ps.Calls,
				Errors:    ps.Errors,
				AvgMs:     float64(ps.AvgLatency.Microseconds()) / 1000.0,
				Health:    "?",
			}
			if h, ok := health[ps.Name]; ok {
				if h.OK {
					row.Health = "ok"
				} else {
					row.Health = "down"
					row.HealthErr = h.LastError
				}
			}
			data.Plugins = append(data.Plugins, row)
		}
		sort.Slice(data.Plugins, func(i, j int) bool { return data.Plugins[i].Name < data.Plugins[j].Name })
	}

	var mbuf bytes.Buffer
	metrics.Render(&mbuf)
	data.Metrics = mbuf.String()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := adminTemplate.Execute(w, data); err != nil {
		http.Error(w, fmt.Sprintf("template error: %v", err), http.StatusInternalServerError)
	}
}

func defaultIfEmpty(s, fallback string) string {
	if s == "" {
		return fallback
	}
	return s
}

var adminTemplate = template.Must(template.New("admin").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<title>wave — admin</title>
<style>
  :root { color-scheme: light dark; }
  body { font: 14px/1.4 system-ui, -apple-system, sans-serif; max-width: 1100px; margin: 2em auto; padding: 0 1em; }
  h1 { margin: 0 0 0.2em; font-size: 1.6em; }
  h2 { margin: 1.6em 0 0.4em; font-size: 1.1em; border-bottom: 1px solid #8884; padding-bottom: 0.2em; }
  table { width: 100%; border-collapse: collapse; margin-bottom: 1em; }
  th, td { text-align: left; padding: 4px 8px; border-bottom: 1px solid #8882; vertical-align: top; }
  th { font-weight: 600; }
  td.num { text-align: right; font-variant-numeric: tabular-nums; }
  code { background: #8881; padding: 0 4px; border-radius: 3px; }
  pre { background: #8881; padding: 0.8em; border-radius: 4px; overflow-x: auto; max-height: 360px; font-size: 12px; }
  .muted { color: #888; }
  .meta { display: flex; gap: 1.6em; flex-wrap: wrap; }
  .meta div { font-size: 13px; }
  .pill { display: inline-block; padding: 1px 6px; border-radius: 8px; background: #8881; font-size: 11px; }
</style>
<meta http-equiv="refresh" content="5">
</head>
<body>
<h1>wave</h1>
<div class="meta">
  <div><strong>{{.Hostname}}</strong></div>
  <div>uptime <strong>{{.UptimeSec}}s</strong></div>
  <div>version <strong>{{.Version}}</strong></div>
</div>

<h2>Routes ({{len .Routes}})</h2>
{{if .Routes}}
<table>
  <thead><tr><th>Method</th><th>Path</th><th>Type</th><th>Auth</th><th>Description</th></tr></thead>
  <tbody>
  {{range .Routes}}
    <tr>
      <td><code>{{.Method}}</code></td>
      <td><code>{{.Path}}</code></td>
      <td><span class="pill">{{.Type}}</span></td>
      <td>{{range .Auth}}<span class="pill">{{.}}</span> {{end}}</td>
      <td class="muted">{{.Description}}</td>
    </tr>
  {{end}}
  </tbody>
</table>
{{else}}<p class="muted">none configured.</p>{{end}}

<h2>Connections ({{len .Connections}})</h2>
{{if .Connections}}
<table>
  <thead><tr><th>Name</th><th>Type</th><th>Subscribe path</th><th class="num">Subscribers</th><th class="num">Published</th><th class="num">Dropped</th></tr></thead>
  <tbody>
  {{range .Connections}}
    <tr>
      <td>{{.Name}}</td>
      <td><span class="pill">{{.Type}}</span></td>
      <td><code>{{.Path}}</code></td>
      <td class="num">{{.Subscribers}}</td>
      <td class="num">{{.Published}}</td>
      <td class="num">{{.Dropped}}</td>
    </tr>
  {{end}}
  </tbody>
</table>
{{else}}<p class="muted">none configured.</p>{{end}}

<h2>Plugins ({{len .Plugins}})</h2>
{{if .Plugins}}
<table>
  <thead><tr><th>Name</th><th>Transport</th><th>Health</th><th class="num">Calls</th><th class="num">Errors</th><th class="num">Avg ms</th></tr></thead>
  <tbody>
  {{range .Plugins}}
    <tr>
      <td>{{.Name}}</td>
      <td><span class="pill">{{.Transport}}</span></td>
      <td>
        {{if eq .Health "ok"}}<span class="pill" style="background:#4eb46644;color:#2e7a3a">ok</span>
        {{else if eq .Health "down"}}<span class="pill" style="background:#d3535344;color:#b03a3a" title="{{.HealthErr}}">down</span>
        {{else}}<span class="pill">?</span>{{end}}
      </td>
      <td class="num">{{.Calls}}</td>
      <td class="num">{{.Errors}}</td>
      <td class="num">{{printf "%.2f" .AvgMs}}</td>
    </tr>
  {{end}}
  </tbody>
</table>
{{else}}<p class="muted">none configured.</p>{{end}}

<h2>Metrics</h2>
<pre>{{.Metrics}}</pre>
<p class="muted">Auto-refreshes every 5 seconds. Raw metrics at <a href="/metrics">/metrics</a>. Health at <a href="/healthz">/healthz</a> · <a href="/readyz">/readyz</a>.</p>
</body>
</html>
`))
