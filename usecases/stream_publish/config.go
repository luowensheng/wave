// Package stream_publish implements `type: stream-publish` route handlers.
//
// At request time the handler:
//  1. Resolves the named broker from the global connections.Registry.
//  2. Parses the incoming JSON body.
//  3. Filters fields via output: { name: jsonpath } and merges static_meta.
//  4. Formats an SSE frame and publishes it to the broker.
//  5. Optionally fires-and-forgets a copy to forward_url.
package stream_publish

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"wave/infra/connections"
	"wave/infra/webhooksig"
	"wave/usecases/jsonpath"
)

type Config struct {
	Connection    string            `yaml:"connection,omitempty" json:"connection,omitempty"`
	RouteID       string            `yaml:"route_id,omitempty" json:"route_id,omitempty"`
	EventType     string            `yaml:"event_type,omitempty" json:"event_type,omitempty"`
	Output        map[string]string `yaml:"output,omitempty" json:"output,omitempty"`
	StaticMeta    map[string]string `yaml:"static_meta,omitempty" json:"static_meta,omitempty"`
	ExcludeFields []string          `yaml:"exclude_fields,omitempty" json:"exclude_fields,omitempty"`
	ForwardURL    string            `yaml:"forward_url,omitempty" json:"forward_url,omitempty"`

	// ForwardSign optionally signs the forwarded request with HMAC so
	// the downstream receiver can verify it. Pair with the matching
	// webhooksig verifier on the receiver.
	ForwardSign *ForwardSignConfig `yaml:"forward_sign,omitempty" json:"forward_sign,omitempty"`
}

// ForwardSignConfig is the YAML shape for outbound HMAC signing on
// forward_url. Mirrors webhooksig.SignConfig but is YAML-friendly.
type ForwardSignConfig struct {
	Provider     string `yaml:"provider,omitempty" json:"provider,omitempty"`
	Secret       string `yaml:"secret,omitempty" json:"secret,omitempty"`
	Header       string `yaml:"header,omitempty" json:"header,omitempty"`
	Algorithm    string `yaml:"algorithm,omitempty" json:"algorithm,omitempty"`
	HeaderPrefix string `yaml:"header_prefix,omitempty" json:"header_prefix,omitempty"`
}

// CreateRoute implements servers.RouteConfig.
func (c *Config) CreateRoute(method, path string, args map[string]string) (http.HandlerFunc, error) {
	if c == nil {
		return nil, fmt.Errorf("nil stream-publish config")
	}
	if c.Connection == "" {
		return nil, fmt.Errorf("stream-publish missing `connection`")
	}

	return func(w http.ResponseWriter, r *http.Request) {
		registry := connections.Default()
		if registry == nil {
			http.Error(w, "connections registry not initialized", http.StatusInternalServerError)
			return
		}
		broker, ok := registry.Get(c.Connection)
		if !ok {
			http.Error(w, fmt.Sprintf("connection %q not found", c.Connection), http.StatusInternalServerError)
			return
		}

		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4<<20))
		if err != nil {
			http.Error(w, "request body too large", http.StatusRequestEntityTooLarge)
			return
		}
		_ = r.Body.Close()

		// Wrap the incoming payload so users can write `response.id`,
		// matching the spec exactly. Also keep a top-level alias for the
		// common case where users write a bare `id`.
		envelope := map[string]json.RawMessage{
			"response": json.RawMessage(raw),
		}
		envBytes, _ := json.Marshal(envelope)

		filtered := jsonpath.Apply(envBytes, c.Output, c.StaticMeta)
		// If no `output` mapping was provided we fall back to forwarding
		// the raw payload, minus exclude_fields.
		if len(c.Output) == 0 {
			var asMap map[string]any
			if err := json.Unmarshal(raw, &asMap); err == nil {
				for _, ex := range c.ExcludeFields {
					delete(asMap, ex)
				}
				for k, v := range c.StaticMeta {
					asMap[k] = v
				}
				filtered = asMap
			}
		}

		dataBytes, err := json.Marshal(filtered)
		if err != nil {
			http.Error(w, "failed to encode filtered payload", http.StatusInternalServerError)
			return
		}

		broker.Publish(formatSSEFrame(c.EventType, dataBytes))

		if c.ForwardURL != "" {
			// Run on a goroutine so we don't block the broker fan-out
			// even when the outbox is in inline-fallback mode.
			go c.forward(r.Context(), raw)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte(`{"received":true}`))
	}, nil
}

// formatSSEFrame builds a single Server-Sent Events message.
//
//	event: <type>     (only when EventType is non-empty)
//	data:  <json>
//	\n
func formatSSEFrame(eventType string, data []byte) []byte {
	var b bytes.Buffer
	if eventType != "" {
		fmt.Fprintf(&b, "event: %s\n", eventType)
	}
	b.WriteString("data: ")
	b.Write(data)
	b.WriteString("\n\n")
	return b.Bytes()
}

// forward fires the original payload at an audit/log endpoint.
// Best effort; errors are intentionally swallowed.
//
// Two delivery paths:
//   - If a global outbox is registered (see SetDefaultOutbox), the
//     delivery is enqueued for durable retry.
//   - Otherwise we POST inline (legacy fire-and-forget behavior).
//
// In both cases, if signing is configured, the body is signed and the
// resulting auth header travels with the request.
var forwardClient = &http.Client{Timeout: 5 * time.Second}

// Outbox is the minimal interface stream-publish needs. Decouples this
// package from infra/outbox to avoid an import cycle.
type Outbox interface {
	Enqueue(ctx context.Context, url string, body []byte, headers map[string]string) error
}

var (
	outboxMu  sync.RWMutex
	outboxRef Outbox
)

// SetDefaultOutbox installs an outbox so future stream-publish forwards
// become durable. nil restores fire-and-forget mode.
func SetDefaultOutbox(o Outbox) {
	outboxMu.Lock()
	defer outboxMu.Unlock()
	outboxRef = o
}

func defaultOutbox() Outbox {
	outboxMu.RLock()
	defer outboxMu.RUnlock()
	return outboxRef
}

func (c *Config) forward(ctx context.Context, payload []byte) {
	headers := map[string]string{"Content-Type": "application/json"}
	if c.ForwardSign != nil && c.ForwardSign.Provider != "" {
		req, _ := http.NewRequest(http.MethodPost, c.ForwardURL, bytes.NewReader(payload))
		_ = webhooksig.SignRequest(req, payload, webhooksig.SignConfig{
			Provider:     c.ForwardSign.Provider,
			Secret:       c.ForwardSign.Secret,
			Header:       c.ForwardSign.Header,
			Algorithm:    c.ForwardSign.Algorithm,
			HeaderPrefix: c.ForwardSign.HeaderPrefix,
		})
		// Lift the signing headers onto the outgoing request whether we
		// go through the outbox or fire inline.
		for _, k := range []string{
			"Stripe-Signature", "X-Hub-Signature-256",
			"X-Slack-Signature", "X-Slack-Request-Timestamp",
			c.ForwardSign.Header,
		} {
			if v := req.Header.Get(k); v != "" {
				headers[k] = v
			}
		}
	}

	if ob := defaultOutbox(); ob != nil {
		_ = ob.Enqueue(ctx, c.ForwardURL, payload, headers)
		return
	}

	// Fallback: inline POST (no durability).
	req, err := http.NewRequest(http.MethodPost, c.ForwardURL, bytes.NewReader(payload))
	if err != nil {
		return
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := forwardClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}
