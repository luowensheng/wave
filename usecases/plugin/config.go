// Package plugin implements `type: plugin` route handlers.
//
// At request time the handler:
//  1. Resolves the named plugin from the global plugins.Registry.
//  2. Builds a plugins.Request from the incoming HTTP request, optionally
//     filtering headers via include_headers.
//  3. Calls the transport-agnostic plugin client.
//  4. Applies response_output filtering via usecases/jsonpath.
//  5. Maps response_headers from the plugin response to the client response.
package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/luowensheng/wave/infra/plugins"
	"github.com/luowensheng/wave/usecases/jsonpath"
)

type Config struct {
	Name             string            `yaml:"name,omitempty" json:"name,omitempty"`
	TriggerKey       string            `yaml:"trigger_key,omitempty" json:"trigger_key,omitempty"`
	ForwardBody      *bool             `yaml:"forward_body,omitempty" json:"forward_body,omitempty"`
	IncludeHeaders   []string          `yaml:"include_headers,omitempty" json:"include_headers,omitempty"`
	TransformRequest *bool             `yaml:"transform_request,omitempty" json:"transform_request,omitempty"`
	EnvOverrides     map[string]string `yaml:"env_overrides,omitempty" json:"env_overrides,omitempty"`
	ResponseOutput   map[string]string `yaml:"response_output,omitempty" json:"response_output,omitempty"`
	ResponseHeaders  map[string]string `yaml:"response_headers,omitempty" json:"response_headers,omitempty"`
}

// CreateRoute implements servers.RouteConfig.
func (c *Config) CreateRoute(method, path string, args map[string]string) (http.HandlerFunc, error) {
	if c == nil {
		return nil, fmt.Errorf("nil plugin config")
	}
	if c.Name == "" {
		return nil, fmt.Errorf("plugin route missing `name`")
	}

	return func(w http.ResponseWriter, r *http.Request) {
		registry := plugins.Default()
		if registry == nil {
			http.Error(w, "plugin registry not initialized", http.StatusInternalServerError)
			return
		}
		client, ok := registry.Get(c.Name)
		if !ok {
			http.Error(w, fmt.Sprintf("plugin %q not found", c.Name), http.StatusInternalServerError)
			return
		}

		req := c.buildRequest(r)
		resp, err := client.Call(r.Context(), req)
		if err != nil {
			http.Error(w, fmt.Sprintf("plugin error: %v", err), http.StatusBadGateway)
			return
		}
		c.writeResponse(w, resp)
	}, nil
}

// extractPathParams pulls every {key} value the Go 1.22 ServeMux pattern
// captured from the request. The mux exposes them via r.PathValue, but
// without the original pattern in hand we don't know the key names —
// so we walk the path template stored on the route's pattern.
//
// We get the pattern by stripping a leading "<METHOD> " from the
// originally-registered pattern, but that's known only at boot time.
// Workaround: scan the registered pattern out of r.Pattern (Go 1.22+).
func extractPathParams(r *http.Request) map[string]string {
	if r.Pattern == "" {
		return nil
	}
	// The pattern looks like "POST /items/{id}/c/{cid}"; lift the keys.
	out := map[string]string{}
	p := r.Pattern
	for {
		i := strings.IndexByte(p, '{')
		if i < 0 {
			break
		}
		j := strings.IndexByte(p[i:], '}')
		if j < 0 {
			break
		}
		key := p[i+1 : i+j]
		if key != "" {
			if v := r.PathValue(key); v != "" {
				out[key] = v
			}
		}
		p = p[i+j+1:]
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// buildRequest serializes the HTTP request into the plugin contract.
func (c *Config) buildRequest(r *http.Request) *plugins.Request {
	headers := map[string]string{}
	if len(c.IncludeHeaders) == 0 {
		for k, v := range r.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
	} else {
		for _, name := range c.IncludeHeaders {
			if v := r.Header.Get(name); v != "" {
				headers[name] = v
			}
		}
	}

	cookies := map[string]string{}
	for _, ck := range r.Cookies() {
		cookies[ck.Name] = ck.Value
	}

	query := map[string]string{}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			query[k] = v[0]
		}
	}

	var body json.RawMessage
	if c.ForwardBody == nil || *c.ForwardBody {
		raw, _ := io.ReadAll(http.MaxBytesReader(nil, r.Body, 8<<20)) // 8 MiB cap
		_ = r.Body.Close()
		raw = []byte(strings.TrimSpace(string(raw)))
		if len(raw) > 0 {
			// Forward raw bytes as-is. If it's not valid JSON the plugin can
			// re-decode from string; the contract is `bytes` semantically.
			if json.Valid(raw) {
				body = json.RawMessage(raw)
			} else {
				quoted, _ := json.Marshal(string(raw))
				body = json.RawMessage(quoted)
			}
		}
	}

	return &plugins.Request{
		TriggerKey: c.TriggerKey,
		Metadata: map[string]string{
			"route_path": r.URL.Path,
			"method":     r.Method,
			"remote_ip":  clientIP(r),
		},
		Headers:    headers,
		Cookies:    cookies,
		Query:      query,
		PathParams: extractPathParams(r),
		Body:       body,
	}
}

// writeResponse maps the plugin Response onto the HTTP ResponseWriter,
// applying response_output filtering and response_headers mapping.
func (c *Config) writeResponse(w http.ResponseWriter, resp *plugins.Response) {
	// Headers come first.
	for k, v := range resp.Headers {
		w.Header().Set(k, v)
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	// response_headers mapping is a *whitelist* / rename pass. With the
	// current plugin contract the source value lives in resp.Headers under
	// the key the caller named on the right; the simple form is a passthrough.
	for outName, srcName := range c.ResponseHeaders {
		if v, ok := resp.Headers[srcName]; ok {
			w.Header().Set(outName, v)
		}
	}

	body := resp.Body
	if len(c.ResponseOutput) > 0 && len(body) > 0 {
		// Wrap the response body under "response" so callers can write
		// `success: response.success` consistently with the spec.
		envelope := map[string]json.RawMessage{"response": body}
		envBytes, _ := json.Marshal(envelope)
		filtered := jsonpath.Apply(envBytes, c.ResponseOutput, nil)
		if out, err := json.Marshal(filtered); err == nil {
			body = out
		}
	}

	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	if len(body) > 0 {
		_, _ = w.Write(body)
	}
}

// clientIP extracts a best-effort client IP from common forwarding headers.
func clientIP(r *http.Request) string {
	if v := r.Header.Get("X-Real-IP"); v != "" {
		return v
	}
	if v := r.Header.Get("X-Forwarded-For"); v != "" {
		if i := strings.IndexByte(v, ','); i >= 0 {
			return strings.TrimSpace(v[:i])
		}
		return strings.TrimSpace(v)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		return host[:i]
	}
	return host
}

var _ = context.Background // keep context import in case future versions need it
var _ = strings.Contains   // path-param extractor uses strings
