package common

import (
	"gopkg.in/yaml.v3"
)

// RawYAML wraps yaml.Node to behave like json.RawMessage
type RawYAML struct {
	Node yaml.Node
}

// Implement yaml.Unmarshaler
func (r *RawYAML) UnmarshalYAML(value *yaml.Node) error {
	r.Node = *value
	return nil
}

// Convert back to bytes
func (r *RawYAML) Bytes() ([]byte, error) {
	return yaml.Marshal(&r.Node)
}
