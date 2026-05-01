package servers

import (
	"io"
	"strings"
	"text/template"
)

const routeTemplate = `
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>API Routes</title>
    <style>
        :root {
            --primary: #4a6cf7;
            --primary-light: #c7d2fe;
            --dark: #1e293b;
            --light: #f8fafc;
            --gray: #94a3b8;
            --success: #10b981;
            --warning: #f59e0b;
            --danger: #ef4444;
            --border: #e2e8f0;
        }
        
        * {
            margin: 0;
            padding: 0;
            box-sizing: border-box;
        }
        
        body {
            font-family: -apple-system, BlinkMacSystemFont, 'Segoe UI', Roboto, sans-serif;
            background-color: var(--light);
            color: var(--dark);
            line-height: 1.6;
            padding: 2rem;
        }
        
        .container {
            max-width: 1200px;
            margin: 0 auto;
        }
        
        header {
            text-align: center;
            margin-bottom: 2.5rem;
        }
        
        h1 {
            font-size: 2.5rem;
            font-weight: 700;
            margin-bottom: 0.5rem;
            color: var(--primary);
        }
        
        .subtitle {
            color: var(--gray);
            font-size: 1.1rem;
        }
        
        .routes-table {
            width: 100%;
            border-collapse: collapse;
            background: white;
            border-radius: 12px;
            overflow: hidden;
            box-shadow: 0 4px 6px -1px rgba(0, 0, 0, 0.1), 0 2px 4px -1px rgba(0, 0, 0, 0.06);
        }
        
        .routes-table th {
            background-color: var(--primary);
            color: white;
            text-align: left;
            padding: 1.25rem 1.5rem;
            font-weight: 600;
            text-transform: uppercase;
            letter-spacing: 0.5px;
            font-size: 0.85rem;
        }
        
        .routes-table td {
            padding: 1.25rem 1.5rem;
            border-bottom: 1px solid var(--border);
        }
        
        .routes-table tr:last-child td {
            border-bottom: none;
        }
        
        .routes-table tr:hover td {
            background-color: #f1f5f9;
        }
        
        .method {
            display: inline-block;
            padding: 0.25rem 0.75rem;
            border-radius: 20px;
            font-size: 0.85rem;
            font-weight: 600;
            text-transform: uppercase;
            letter-spacing: 0.5px;
        }
        
        .method-get { background-color: var(--success); color: white; }
        .method-post { background-color: var(--primary); color: white; }
        .method-put { background-color: var(--warning); color: white; }
        .method-patch { background-color: var(--primary-light); color: var(--dark); }
        .method-delete { background-color: var(--danger); color: white; }
        .method-head { background-color: var(--gray); color: white; }
        .method-options { background-color: #cbd5e1; color: var(--dark); }
        
        .path {
            font-family: 'SFMono-Regular', Consolas, 'Liberation Mono', Menlo, monospace;
            font-size: 0.95rem;
            color: var(--primary);
            font-weight: 600;
        }
        
        .description {
            color: var(--dark);
            max-width: 400px;
        }
        
        .empty-state {
            text-align: center;
            padding: 3rem;
            color: var(--gray);
        }
        
        @media (max-width: 768px) {
            body {
                padding: 1rem;
            }
            
            .routes-table th,
            .routes-table td {
                padding: 1rem;
            }
            
            .description {
                max-width: 200px;
            }
        }
    </style>
</head>
<body>
    <div class="container">
        <header>
            <h1>API Routes</h1>
            <p class="subtitle">Available endpoints and their specifications</p>
        </header>
        
        {{if .Routes}}
        <table class="routes-table">
            <thead>
                <tr>
                    <th>Method</th>
                    <th>Path</th>
                    <th>Type</th>
                    <th>Description</th>
                </tr>
            </thead>
            <tbody>
                {{range .Routes}}
                <tr>
                    <td>
                        <span class="method method-{{lower .Method}}">{{.Method}}</span>
                    </td>
                    <td class="path">{{.Path}}</td>
                    <td>{{.Type}}</td>
                    <td class="description">{{.Description}}</td>
                </tr>
                {{end}}
            </tbody>
        </table>
        {{else}}
        <div class="empty-state">
            <h2>No routes available</h2>
            <p>There are no API routes to display at this time.</p>
        </div>
        {{end}}
    </div>
</body>
</html>
`



type PageData struct {
	Routes []RouteSummary
}

func renderDiscovery(w io.Writer, routes []RouteSummary) error {

	tmpl := template.Must(template.New("routes").Funcs(template.FuncMap{
		"lower": strings.ToLower,
	}).Parse(routeTemplate))

	data := PageData{Routes: routes}
	return tmpl.Execute(w, data)
}
