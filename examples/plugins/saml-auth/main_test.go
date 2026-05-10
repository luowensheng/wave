package main

import (
	"context"
	"strings"
	"testing"

	sdk "wave.dev/sdk"
)

// Without the samlintegration build tag, only smoke-test the plugin.
// Real SAML flows require an IdP — those live behind the build tag.

func TestSamlPlugin_UnknownMethodRejected(t *testing.T) {
	p := newSamlPlugin()
	_, err := p.Authenticate(context.Background(), &sdk.AuthRequest{Method: "totally-unknown"})
	if err == nil || !strings.Contains(err.Error(), "unsupported method") {
		t.Fatalf("expected unsupported-method error, got %v", err)
	}
}

func TestSamlPlugin_LogoutAlwaysSucceeds(t *testing.T) {
	p := newSamlPlugin()
	if err := p.Logout(context.Background(), "alice"); err != nil {
		t.Fatalf("Logout returned %v, want nil", err)
	}
}

func TestSamlPlugin_RefreshClaimsUncached(t *testing.T) {
	p := newSamlPlugin()
	if _, err := p.RefreshClaims(context.Background(), "missing"); err == nil {
		t.Fatal("expected error for uncached subject")
	}
}
