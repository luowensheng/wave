package servers

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"wave/infra/observability"
	"wave/infra/plugins"
)

// fakeExporter is a tiny in-memory exporter that records everything
// it sees. Implements observability.PluginExporter directly (the
// orchestrator-level kindsExporterAdapter is exercised in the boot
// path; here we go straight through the fanout to keep the test
// hermetic — no subprocess, no JSON-RPC).
type fakeExporter struct {
	mu      sync.Mutex
	metrics []*observability.Sample
	closed  atomic.Bool
}

func (f *fakeExporter) ExportMetrics(b []*observability.Sample) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.metrics = append(f.metrics, b...)
	return nil
}
func (f *fakeExporter) ExportLogs(b []*observability.LogRecord) error { return nil }
func (f *fakeExporter) ExportTraces(b []*observability.Span) error    { return nil }
func (f *fakeExporter) Close() error                                  { f.closed.Store(true); return nil }

func (f *fakeExporter) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.metrics)
}

// TestObservability_FanoutFromDefault wires Default() through Tee +
// Fanout and asserts that an EmitMetric reaches every fake exporter
// within the flush window. Validates the emission contract used by
// the request middleware, without spinning up a real HTTP server.
func TestObservability_FanoutFromDefault(t *testing.T) {
	a, b := &fakeExporter{}, &fakeExporter{}
	fan := observability.NewFanout(map[string]observability.PluginExporter{
		"a": a, "b": b,
	},
		observability.WithBatchSize(1),
		observability.WithFlushPeriod(10*time.Millisecond),
	)
	defer fan.Close()
	prom := observability.NewPrometheusSink()
	prev := observability.Default()
	observability.SetDefault(observability.NewTee(prom, fan))
	defer observability.SetDefault(prev)

	for i := 0; i < 5; i++ {
		observability.Default().EmitMetric(&observability.Sample{
			Name:  "wave_http_requests_total",
			Type:  "counter",
			Value: 1,
			Labels: map[string]string{
				"method": "GET", "status": "200",
			},
		})
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if a.count() >= 5 && b.count() >= 5 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if a.count() < 5 || b.count() < 5 {
		t.Fatalf("each subscriber should see 5 samples, got a=%d b=%d", a.count(), b.count())
	}
}

// TestObservability_ValidateRejectsUnknownExporter ensures boot
// validation catches a plugin name in observability.exporters that
// has no matching plugins:<name> entry.
func TestObservability_ValidateRejectsUnknownExporter(t *testing.T) {
	s := &Server{Config: &Config{
		Plugins: map[string]*plugins.PluginConfig{
			"otel": {Kind: plugins.KindExporter, Transport: "process", Command: "./does-not-matter"},
		},
		Observability: &ObservabilityConfig{Exporters: []string{"otel", "ghost"}},
	}}
	if err := s.validateObservability(); err == nil {
		t.Fatal("expected error for unknown exporter")
	}
}

// TestObservability_ValidateRejectsWrongKind ensures a non-exporter
// plugin can't be listed under observability.exporters.
func TestObservability_ValidateRejectsWrongKind(t *testing.T) {
	s := &Server{Config: &Config{
		Plugins: map[string]*plugins.PluginConfig{
			"thing": {Kind: plugins.KindHandler, Transport: "process", Command: "./x"},
		},
		Observability: &ObservabilityConfig{Exporters: []string{"thing"}},
	}}
	if err := s.validateObservability(); err == nil {
		t.Fatal("expected kind-mismatch error")
	}
}

func TestObservability_ValidateNoOpWhenAbsent(t *testing.T) {
	s := &Server{Config: &Config{}}
	if err := s.validateObservability(); err != nil {
		t.Fatalf("absent block must be ok: %v", err)
	}
}
