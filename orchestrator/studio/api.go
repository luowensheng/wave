package studio

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/luowensheng/wave/orchestrator/scaffold"
)

// apiHandler returns the http.Handler that serves /api/...
func apiHandler(reg *Registry, sup *Supervisor) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("/api/projects", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			listProjects(w, reg, sup)
		case http.MethodPost:
			addProject(w, r, reg)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/projects/scaffold", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		scaffoldProject(w, r, reg)
	})

	mux.HandleFunc("/api/projects/", func(w http.ResponseWriter, r *http.Request) {
		// /api/projects/:id[/sub]
		rest := strings.TrimPrefix(r.URL.Path, "/api/projects/")
		parts := strings.SplitN(rest, "/", 2)
		id := parts[0]
		sub := ""
		if len(parts) == 2 {
			sub = parts[1]
		}
		p, ok := reg.Get(id)
		if !ok {
			http.Error(w, "project not found", http.StatusNotFound)
			return
		}
		switch sub {
		case "":
			if r.Method == http.MethodDelete {
				if err := reg.Remove(id); err != nil {
					http.Error(w, err.Error(), http.StatusBadRequest)
					return
				}
				_ = sup.Stop(id)
				writeJSON(w, http.StatusOK, map[string]string{"status": "removed"})
				return
			}
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		case "start":
			if err := sup.Start(p); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, projectStatus(sup, p))
		case "stop":
			if err := sup.Stop(id); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, projectStatus(sup, p))
		case "restart":
			if err := sup.Restart(id, p); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, projectStatus(sup, p))
		case "status":
			writeJSON(w, http.StatusOK, projectStatus(sup, p))
		case "logs":
			streamLogs(w, r, sup, p)
		case "routes":
			pc, err := probe(p.ConfigPath())
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"host":   pc.Host,
				"port":   pc.Port,
				"routes": pc.Routes,
			})
		case "test-route":
			testRoute(w, r, p)
		case "metrics":
			proxyMetrics(w, p)
		default:
			http.Error(w, "not found", http.StatusNotFound)
		}
	})

	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func projectStatus(sup *Supervisor, p *Project) map[string]any {
	out := map[string]any{
		"id":       p.ID,
		"name":     p.Name,
		"path":     p.Path,
		"status":   StatusStopped,
		"pid":      0,
		"uptime":   0,
		"restarts": 0,
	}
	if proc, ok := sup.Status(p.ID); ok {
		proc.mu.Lock()
		out["status"] = proc.Status
		out["pid"] = proc.PID
		out["restarts"] = proc.Restarts
		if proc.Status == StatusRunning {
			out["uptime"] = int64(time.Since(proc.StartedAt).Seconds())
		}
		proc.mu.Unlock()
	}
	return out
}

func listProjects(w http.ResponseWriter, reg *Registry, sup *Supervisor) {
	projs := reg.List()
	out := make([]map[string]any, 0, len(projs))
	for _, p := range projs {
		entry := projectStatus(sup, p)
		entry["config_file"] = p.ConfigFile
		entry["added_at"] = p.AddedAt
		out = append(out, entry)
	}
	writeJSON(w, http.StatusOK, map[string]any{"projects": out})
}

func addProject(w http.ResponseWriter, r *http.Request, reg *Registry) {
	var req struct {
		Path       string `json:"path"`
		ConfigFile string `json:"config_file"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.ConfigFile == "" {
		req.ConfigFile = "server.yaml"
	}
	p, err := reg.Add(req.Path, req.ConfigFile)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func scaffoldProject(w http.ResponseWriter, r *http.Request, reg *Registry) {
	var req struct {
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		ParentDir string `json:"parent_dir"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Name == "" || req.Kind == "" || req.ParentDir == "" {
		http.Error(w, "name, kind, parent_dir required", http.StatusBadRequest)
		return
	}
	tpl, ok := scaffold.Get(req.Kind)
	if !ok {
		http.Error(w, "unknown template kind: "+req.Kind, http.StatusBadRequest)
		return
	}
	dest := req.ParentDir + "/" + req.Name
	if err := scaffold.Render(tpl, dest, false); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	p, err := reg.Add(dest, "server.yaml")
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func streamLogs(w http.ResponseWriter, r *http.Request, sup *Supervisor, p *Project) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	ch, cancel, err := sup.Subscribe(p.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer cancel()
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()
	ctx := r.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case line, ok := <-ch:
			if !ok {
				return
			}
			fmt.Fprintf(w, "data: %s\n\n", line)
			flusher.Flush()
		}
	}
}

func testRoute(w http.ResponseWriter, r *http.Request, p *Project) {
	var req struct {
		Method  string            `json:"method"`
		Path    string            `json:"path"`
		Headers map[string]string `json:"headers"`
		Body    string            `json:"body"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Method == "" {
		req.Method = "GET"
	}
	pc, err := probe(p.ConfigPath())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	resp, err := proxyToServer(pc.Host, pc.Port, req.Method, req.Path, req.Headers, req.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func proxyMetrics(w http.ResponseWriter, p *Project) {
	pc, err := probe(p.ConfigPath())
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	url := fmt.Sprintf("http://%s:%d/metrics", pc.Host, pc.Port)
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(resp.StatusCode)
	_, _ = w.Write(body)
}
