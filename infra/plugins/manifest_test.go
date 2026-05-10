package plugins

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadManifestRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	in := Manifest{
		Name:          "demo",
		Kind:          KindStorage,
		Version:       "0.0.1",
		MinWave: "0.4.0",
		Description:   "demo plugin",
		ConfigSchema:  json.RawMessage(`{"type":"object"}`),
	}
	b, _ := json.Marshal(in)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if got.Name != in.Name || got.Kind != in.Kind || got.Version != in.Version {
		t.Errorf("round-trip mismatch: %+v", got)
	}
	if string(got.ConfigSchema) != `{"type":"object"}` {
		t.Errorf("config schema lost: %s", got.ConfigSchema)
	}
}

func TestLoadManifestRejectsUnknownKind(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	_ = os.WriteFile(path, []byte(`{"name":"x","kind":"bogus","version":"1"}`), 0o644)
	if _, err := LoadManifest(path); err == nil {
		t.Error("expected error for unknown kind")
	}
}

func TestLoadManifestDefaultsKindHandler(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	_ = os.WriteFile(path, []byte(`{"name":"x","version":"1"}`), 0o644)
	m, err := LoadManifest(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Kind != KindHandler {
		t.Errorf("expected handler default, got %q", m.Kind)
	}
}
