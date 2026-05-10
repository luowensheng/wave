package servers

import (
	"strings"
	"testing"

	authfeature "wave/orchestrator/features/auth"
	"wave/infra/plugins"
)

// installRegistry sets a registry for the duration of t.
func installRegistry(t *testing.T, configs map[string]*plugins.PluginConfig) {
	t.Helper()
	prev := plugins.Default()
	reg, err := plugins.NewRegistry(configs)
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	plugins.SetDefault(reg)
	t.Cleanup(func() { plugins.SetDefault(prev) })
}

func TestValidateAuthRefs_MissingPluginField(t *testing.T) {
	s := &Server{Config: &Config{
		Auth: map[string]*authfeature.AuthConfig{
			"sso": {Type: "plugin"},
		},
	}}
	err := s.validateAuthRefs()
	if err == nil || !strings.Contains(err.Error(), "requires a `plugin:` field") {
		t.Fatalf("expected missing-plugin-field error, got %v", err)
	}
}

func TestValidateAuthRefs_UnknownPlugin(t *testing.T) {
	installRegistry(t, map[string]*plugins.PluginConfig{})
	s := &Server{Config: &Config{
		Auth: map[string]*authfeature.AuthConfig{
			"sso": {Type: "plugin", Plugin: "nope"},
		},
	}}
	err := s.validateAuthRefs()
	if err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("expected unknown-plugin error, got %v", err)
	}
}

func TestValidateAuthRefs_WrongKind(t *testing.T) {
	installRegistry(t, map[string]*plugins.PluginConfig{
		"some_storage": {
			Transport: "process",
			Command:   "/usr/bin/true",
			Kind:      plugins.KindStorage,
		},
	})
	s := &Server{Config: &Config{
		Auth: map[string]*authfeature.AuthConfig{
			"sso": {Type: "plugin", Plugin: "some_storage"},
		},
	}}
	err := s.validateAuthRefs()
	if err == nil {
		t.Fatal("expected wrong-kind error")
	}
}

func TestValidateAuthRefs_ValidPasses(t *testing.T) {
	installRegistry(t, map[string]*plugins.PluginConfig{
		"saml_corp": {
			Transport: "process",
			Command:   "/usr/bin/true",
			Kind:      plugins.KindAuth,
		},
	})
	s := &Server{Config: &Config{
		Auth: map[string]*authfeature.AuthConfig{
			"sso": {Type: "plugin", Plugin: "saml_corp"},
		},
	}}
	if err := s.validateAuthRefs(); err != nil {
		t.Fatalf("expected nil, got %v", err)
	}
}

func TestValidateAuthRefs_NonPluginTypeIgnored(t *testing.T) {
	s := &Server{Config: &Config{
		Auth: map[string]*authfeature.AuthConfig{
			"basic": {Type: "jwt"},
		},
	}}
	if err := s.validateAuthRefs(); err != nil {
		t.Fatalf("non-plugin auth shouldn't error, got %v", err)
	}
}
