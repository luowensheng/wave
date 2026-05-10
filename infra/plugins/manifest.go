package plugins

import (
	"encoding/json"
	"fmt"
	"os"
)

// Manifest is the metadata document a plugin ships at its root as
// `manifest.json`. wave uses it for compatibility checks, admin
// UI display, and (future) JSON-Schema validation of the plugin's own
// config block.
type Manifest struct {
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	Version       string          `json:"version"`
	MinWave string          `json:"min_wave,omitempty"`
	ConfigSchema  json.RawMessage `json:"config_schema,omitempty"` // JSON Schema
	Description   string          `json:"description,omitempty"`
	Homepage      string          `json:"homepage,omitempty"`
	Author        string          `json:"author,omitempty"`
}

// LoadManifest reads and parses a manifest file.
func LoadManifest(path string) (*Manifest, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read manifest %q: %w", path, err)
	}
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("parse manifest %q: %w", path, err)
	}
	if m.Name == "" {
		return nil, fmt.Errorf("manifest %q: missing name", path)
	}
	if m.Kind == "" {
		m.Kind = KindHandler
	}
	if !IsKnownKind(m.Kind) {
		return nil, fmt.Errorf("manifest %q: unknown kind %q", path, m.Kind)
	}
	if m.Version == "" {
		return nil, fmt.Errorf("manifest %q: missing version", path)
	}
	return &m, nil
}
