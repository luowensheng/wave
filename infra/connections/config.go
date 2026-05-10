// Package connections is the SSE/WS broker registry consumed by the
// `type: stream-publish` route and by auto-registered subscribe handlers.
package connections

import (
	"fmt"
	"time"
)

// ConnectionConfig is the YAML-side description of one named connection
// under the top-level `connections:` block.
type ConnectionConfig struct {
	Type                 string   `yaml:"type,omitempty" json:"type,omitempty"`
	SubscribePath        string   `yaml:"subscribe_path,omitempty" json:"subscribe_path,omitempty"`
	SubscribeAuth        []string `yaml:"subscribe_auth,omitempty" json:"subscribe_auth,omitempty"`
	SubscribeCorsOrigins []string `yaml:"subscribe_cors_origins,omitempty" json:"subscribe_cors_origins,omitempty"`
	MaxClients           int      `yaml:"max_clients,omitempty" json:"max_clients,omitempty"`
	BufferSize           int      `yaml:"buffer_size,omitempty" json:"buffer_size,omitempty"`
	KeepAliveInterval    string   `yaml:"keep_alive_interval,omitempty" json:"keep_alive_interval,omitempty"`
	PingInterval         string   `yaml:"ping_interval,omitempty" json:"ping_interval,omitempty"`
}

// Validate returns nil if the config is usable in this build (sse only;
// ws/auto fall back to sse).
func (c *ConnectionConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("nil connection config")
	}
	switch c.Type {
	case "", "sse", "ws", "auto":
		// recognized; ws/auto degrade to SSE in Phase 1
	default:
		return fmt.Errorf("unknown connection type %q", c.Type)
	}
	if c.SubscribePath == "" {
		return fmt.Errorf("connection missing `subscribe_path`")
	}
	return nil
}

func (c *ConnectionConfig) keepAliveDuration() time.Duration {
	if d, err := time.ParseDuration(c.KeepAliveInterval); err == nil && d > 0 {
		return d
	}
	return 15 * time.Second
}

func (c *ConnectionConfig) bufferSize() int {
	if c.BufferSize > 0 {
		return c.BufferSize
	}
	return 64
}

func (c *ConnectionConfig) maxClients() int {
	if c.MaxClients > 0 {
		return c.MaxClients
	}
	return 256
}
