package studio

import (
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	servers "wave/orchestrator/server"
)

// probedConfig is the studio UI's view of a project: address (host:port)
// plus a flat list of route summaries.
//
// Studio routes through servers.ProbeConfig + RouteSummariesFromConfig
// — the SAME resolver the running server uses. This makes the UI fully
// composition-aware: included modules are folded in, route prefixes are
// applied, externs are resolved/deduped, and a typed-library file is
// surfaced with the explicit "kind:X library, not a server" error
// instead of an empty/misleading route list. ProbeConfig has no
// os.Chdir side effect and boots nothing (no plugins/DBs/listeners),
// so it is safe in Studio's long-running multi-project process.
type probedConfig struct {
	Host   string
	Port   int
	Routes []routeSummary
}

type routeSummary struct {
	Path        string   `json:"path"`
	Method      string   `json:"method"`
	Methods     []string `json:"methods,omitempty"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
}

func probe(configPath string) (*probedConfig, error) {
	cfg, err := servers.ProbeConfig(configPath)
	if err != nil {
		// Includes the explicit kind-library rejection — surface as-is.
		return nil, fmt.Errorf("studio: %w", err)
	}

	pc := &probedConfig{Host: "localhost", Port: 8080}
	if cfg.Defaults != nil {
		if cfg.Defaults.Host != nil && *cfg.Defaults.Host != "" {
			pc.Host = *cfg.Defaults.Host
		}
		if cfg.Defaults.Port != nil && *cfg.Defaults.Port != 0 {
			pc.Port = *cfg.Defaults.Port
		}
	}

	rows, err := servers.RouteSummariesFromConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("studio: route summaries: %w", err)
	}
	pc.Routes = make([]routeSummary, 0, len(rows))
	for _, r := range rows {
		pc.Routes = append(pc.Routes, routeSummary{
			Path:        r.Path,
			Method:      r.Method,
			Type:        r.Type,
			Description: r.Description,
		})
	}
	return pc, nil
}

// proxyToServer issues an HTTP request to the project's running server
// and returns a structured response for the route tester.
func proxyToServer(host string, port int, method, path string, headers map[string]string, body string) (*proxyResponse, error) {
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	url := fmt.Sprintf("http://%s:%d%s", host, port, path)
	var bodyReader io.Reader
	if body != "" {
		bodyReader = strings.NewReader(body)
	}
	req, err := http.NewRequest(strings.ToUpper(method), url, bodyReader)
	if err != nil {
		return nil, err
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	start := time.Now()
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	dur := time.Since(start)
	hdrs := map[string][]string{}
	for k, v := range resp.Header {
		hdrs[k] = v
	}
	return &proxyResponse{
		Status:     resp.StatusCode,
		Headers:    hdrs,
		Body:       string(respBody),
		DurationMS: dur.Milliseconds(),
	}, nil
}

type proxyResponse struct {
	Status     int                 `json:"status"`
	Headers    map[string][]string `json:"headers"`
	Body       string              `json:"body"`
	DurationMS int64               `json:"duration_ms"`
}
