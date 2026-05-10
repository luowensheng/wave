package sdk

import (
	"encoding/json"
	"os"
)

// Manifest is the metadata document a plugin author ships at the root
// of their plugin as `manifest.json`.
type Manifest struct {
	Name          string          `json:"name"`
	Kind          string          `json:"kind"`
	Version       string          `json:"version"`
	MinWave string          `json:"min_wave,omitempty"`
	ConfigSchema  json.RawMessage `json:"config_schema,omitempty"`
	Description   string          `json:"description,omitempty"`
	Homepage      string          `json:"homepage,omitempty"`
	Author        string          `json:"author,omitempty"`
}

// WriteManifest serializes m to path with stable indentation.
func WriteManifest(path string, m *Manifest) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(b, '\n'), 0o644)
}
