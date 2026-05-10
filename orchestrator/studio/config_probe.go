package studio

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// probeConfig parses just enough of a project's server.yaml to power
// the studio UI: address (host:port) plus a flat list of route summaries.
//
// We deliberately do NOT use orchestrator/server.NewServer here — that
// boots plugins, opens DBs, etc. Studio just wants to read the file.
type probedConfig struct {
	Host   string         `yaml:"-"`
	Port   int            `yaml:"-"`
	Routes []routeSummary `yaml:"-"`

	Default *struct {
		Host *string `yaml:"host"`
		Port *int    `yaml:"port"`
	} `yaml:"default"`
	RawRoutes yaml.Node `yaml:"routes"`
}

type routeSummary struct {
	Path        string   `json:"path"`
	Method      string   `json:"method"`
	Methods     []string `json:"methods,omitempty"`
	Type        string   `json:"type"`
	Description string   `json:"description,omitempty"`
}

func probe(configPath string) (*probedConfig, error) {
	bytes, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}
	var pc probedConfig
	if err := yaml.Unmarshal(bytes, &pc); err != nil {
		return nil, fmt.Errorf("studio: parse config: %w", err)
	}
	pc.Host = "localhost"
	pc.Port = 8080
	if pc.Default != nil {
		if pc.Default.Host != nil && *pc.Default.Host != "" {
			pc.Host = *pc.Default.Host
		}
		if pc.Default.Port != nil && *pc.Default.Port != 0 {
			pc.Port = *pc.Default.Port
		}
	}
	pc.Routes = decodeRoutes(&pc.RawRoutes)
	return &pc, nil
}

// decodeRoutes walks a raw yaml node for the `routes:` sequence and
// returns one entry per route. Tolerates partial / unknown fields.
func decodeRoutes(n *yaml.Node) []routeSummary {
	if n == nil || n.Kind == 0 {
		return nil
	}
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		n = n.Content[0]
	}
	var out []routeSummary
	if n.Kind != yaml.SequenceNode {
		return out
	}
	for _, item := range n.Content {
		if item.Kind != yaml.MappingNode {
			continue
		}
		var rs routeSummary
		var foundType string
		known := map[string]bool{}
		for i := 0; i+1 < len(item.Content); i += 2 {
			k := item.Content[i].Value
			v := item.Content[i+1]
			known[k] = true
			switch k {
			case "path":
				rs.Path = v.Value
			case "method":
				rs.Method = v.Value
			case "methods":
				for _, mn := range v.Content {
					rs.Methods = append(rs.Methods, mn.Value)
				}
			case "type":
				rs.Type = v.Value
			case "description":
				rs.Description = v.Value
			}
		}
		if rs.Type == "" {
			// derive from the first sub-block name we recognise
			for i := 0; i+1 < len(item.Content); i += 2 {
				k := item.Content[i].Value
				switch k {
				case "forward", "file", "redirect", "api", "content",
					"static", "plugin", "graphql", "stream-publish",
					"file-server", "process", "dependencies":
					foundType = k
				}
			}
			rs.Type = foundType
		}
		if rs.Method == "" && len(rs.Methods) == 0 {
			rs.Method = "GET"
		}
		out = append(out, rs)
	}
	return out
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
