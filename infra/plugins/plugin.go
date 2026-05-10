// Package plugins defines the transport-agnostic contract used by the
// `type: plugin` route to call out to external code (subprocess, HTTP,
// gRPC, WASM). All transports speak the same Request/Response shape.
package plugins

import (
	"context"
	"encoding/json"
	"fmt"
)

// Request is the JSON payload that every plugin transport receives.
type Request struct {
	TriggerKey string            `json:"trigger_key,omitempty"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	Headers    map[string]string `json:"headers,omitempty"`
	Cookies    map[string]string `json:"cookies,omitempty"`
	Query      map[string]string `json:"query,omitempty"`
	// PathParams contains values extracted from Go 1.22 ServeMux patterns
	// like `/items/{id}/comments/{cid}`. Empty when the route has no
	// pattern placeholders. Plugins use this to drive resource-aware
	// behavior without reparsing the path.
	PathParams map[string]string `json:"path_params,omitempty"`
	Body       json.RawMessage   `json:"body,omitempty"`
}

// Response is the JSON payload every plugin transport must return.
type Response struct {
	Status  int               `json:"status"`
	Headers map[string]string `json:"headers,omitempty"`
	Body    json.RawMessage   `json:"body,omitempty"`
}

// Client is the transport-agnostic interface every plugin implementation
// satisfies. Returned by Registry lookups.
type Client interface {
	Call(ctx context.Context, req *Request) (*Response, error)
	Close() error
}

// ErrNotImplemented is returned by transports that are stubbed in this
// build (gRPC, WASM in Phase 1).
var ErrNotImplemented = fmt.Errorf("plugin transport not implemented in this build")
