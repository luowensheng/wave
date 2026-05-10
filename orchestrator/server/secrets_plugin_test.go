package servers

import (
	"strings"
	"testing"

	"wave/orchestrator/features/auth"
	"wave/infra/secrets"
)

func TestExpandStruct_AuthConfigSecret(t *testing.T) {
	secrets.SetPluginResolver(func(name, uri string) ([]byte, error) {
		if name != "fake" || uri != "k1" {
			t.Errorf("unexpected resolver call name=%q uri=%q", name, uri)
		}
		return []byte("resolved-secret"), nil
	})
	t.Cleanup(func() { secrets.SetPluginResolver(nil) })

	cfg := &Config{
		Auth: map[string]*auth.AuthConfig{
			"x": {Secret: "${PLUGIN:fake:k1}"},
		},
	}
	if err := secrets.ExpandStruct(cfg); err != nil {
		t.Fatalf("expand: %v", err)
	}
	if got := cfg.Auth["x"].Secret; got != "resolved-secret" {
		t.Errorf("auth secret %q", got)
	}
}

func TestFindMarkers_UnresolvedDetected(t *testing.T) {
	cfg := &Config{
		Auth: map[string]*auth.AuthConfig{
			"x": {Secret: "${PLUGIN:typo:abc}"},
		},
	}
	got := secrets.FindMarkers(cfg, "PLUGIN")
	if len(got) != 1 {
		t.Fatalf("expected 1 marker, got %v", got)
	}
	if !strings.Contains(got[0], "typo") {
		t.Errorf("marker text %q", got[0])
	}
}

func TestExpandStruct_NestedConfigPaths(t *testing.T) {
	secrets.SetPluginResolver(func(name, uri string) ([]byte, error) {
		return []byte("R-" + uri), nil
	})
	t.Cleanup(func() { secrets.SetPluginResolver(nil) })

	cfg := &Config{
		Auth: map[string]*auth.AuthConfig{
			"a": {Secret: "${PLUGIN:p:auth1}"},
			"b": {Secret: "${PLUGIN:p:auth2}"},
		},
		OutboxDB: "${PLUGIN:p:db}",
	}
	if err := secrets.ExpandStruct(cfg); err != nil {
		t.Fatal(err)
	}
	if cfg.Auth["a"].Secret != "R-auth1" {
		t.Errorf("a %q", cfg.Auth["a"].Secret)
	}
	if cfg.Auth["b"].Secret != "R-auth2" {
		t.Errorf("b %q", cfg.Auth["b"].Secret)
	}
	if cfg.OutboxDB != "R-db" {
		t.Errorf("outbox %q", cfg.OutboxDB)
	}
}
