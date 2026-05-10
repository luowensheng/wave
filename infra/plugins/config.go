package plugins

import (
	"fmt"
	"time"
)

// PluginConfig is the YAML-side description of a plugin instance under
// the top-level `plugins:` block.
type PluginConfig struct {
	Transport        string            `yaml:"transport,omitempty" json:"transport,omitempty"`
	Address          string            `yaml:"address,omitempty" json:"address,omitempty"`
	ProtoService     string            `yaml:"proto_service,omitempty" json:"proto_service,omitempty"`
	Command          string            `yaml:"command,omitempty" json:"command,omitempty"`
	WasmModule       string            `yaml:"wasm_module,omitempty" json:"wasm_module,omitempty"`
	WasmCapabilities []string          `yaml:"wasm_capabilities,omitempty" json:"wasm_capabilities,omitempty"`
	Timeout          string            `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Env              map[string]string `yaml:"env,omitempty" json:"env,omitempty"`

	// Retries: 0 = no retry (default). N = up to N retries on transient
	// transport errors (subprocess exec failures, HTTP 5xx, network errors).
	// Backoff is exponential starting at 50ms, capped at 2s, with jitter.
	Retries     int    `yaml:"retries,omitempty" json:"retries,omitempty"`
	RetryBackoff string `yaml:"retry_backoff,omitempty" json:"retry_backoff,omitempty"` // initial backoff; default 50ms

	// Kind selects which typed contract this plugin implements.
	// Empty defaults to "handler" for back-compat with existing plugins.
	// When Kind is set to a non-handler value AND Transport=="process",
	// the long-lived JSON-RPC subprocess transport is used instead of the
	// one-shot per-call subprocess transport.
	Kind string `yaml:"kind,omitempty" json:"kind,omitempty"`
}

// timeoutDuration parses Timeout, defaulting to 5s when unset/invalid.
func (c *PluginConfig) timeoutDuration() time.Duration {
	if c == nil || c.Timeout == "" {
		return 5 * time.Second
	}
	if d, err := time.ParseDuration(c.Timeout); err == nil {
		return d
	}
	return 5 * time.Second
}

// Validate returns nil if the config has a transport this build supports.
func (c *PluginConfig) Validate() error {
	if c == nil {
		return fmt.Errorf("nil plugin config")
	}
	switch c.Transport {
	case "process":
		if c.Command == "" {
			return fmt.Errorf("plugin transport=process requires `command`")
		}
	case "http":
		if c.Address == "" {
			return fmt.Errorf("plugin transport=http requires `address`")
		}
	case "grpc", "wasm":
		// recognized but stubbed in this build
	case "":
		return fmt.Errorf("plugin missing `transport`")
	default:
		return fmt.Errorf("unknown plugin transport %q", c.Transport)
	}
	if c.Kind != "" && !IsKnownKind(c.Kind) {
		return fmt.Errorf("unknown plugin kind %q", c.Kind)
	}
	return nil
}
