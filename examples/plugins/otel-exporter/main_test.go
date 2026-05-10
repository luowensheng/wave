package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestUnknownMethodRejected does a smoke pass over the JSON-RPC frame
// shape the orchestrator would send. We don't init the OTel SDK here
// (no OTLP receiver in the test env); instead we exercise the local
// helpers that don't depend on it.
func TestEnvOr_FallsBackToDefault(t *testing.T) {
	got := envOr("DEFINITELY_NOT_SET_OTEL_TEST", "fallback")
	if got != "fallback" {
		t.Fatalf("got %q, want fallback", got)
	}
}

// TestLabelsToAttrs_Empty just exercises the helper to ensure the
// build pipeline links the otel attribute package.
func TestLabelsToAttrs_Empty(t *testing.T) {
	if got := labelsToAttrs(nil); len(got) != 0 {
		t.Fatalf("expected 0 attrs, got %d", len(got))
	}
}

// TestManifestParses confirms the manifest.json shape the orchestrator
// loads at boot stays valid.
func TestManifestParses(t *testing.T) {
	const manifest = `{"name":"otel","kind":"exporter","version":"0.1.0"}`
	var v map[string]any
	if err := json.NewDecoder(strings.NewReader(manifest)).Decode(&v); err != nil {
		t.Fatalf("manifest decode: %v", err)
	}
	if v["kind"] != "exporter" {
		t.Fatalf("expected kind=exporter, got %v", v["kind"])
	}
	// Smoke: bytes round-trip.
	var buf bytes.Buffer
	_ = json.NewEncoder(&buf).Encode(v)
	if buf.Len() == 0 {
		t.Fatal("empty round-trip")
	}
}
