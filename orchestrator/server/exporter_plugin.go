package servers

import (
	"context"
	"fmt"
	"log"

	"wave/infra/observability"
	"wave/infra/plugins"
	"wave/infra/plugins/kinds"
)

// ObservabilityConfig is the top-level YAML block:
//
//	observability:
//	  exporters: [otel, datadog]
//
// Each name MUST resolve to a plugin in `plugins:` whose kind is
// "exporter". An empty/absent block leaves Prometheus-only mode in
// place (back-compat).
type ObservabilityConfig struct {
	Exporters []string `yaml:"exporters,omitempty" json:"exporters,omitempty"`
}

// kindsExporterAdapter bridges kinds.ExporterPlugin (context-aware,
// returns error) → observability.PluginExporter (no ctx, error). The
// fanout owns lifetime, so we use context.Background() with no
// cancellation; future enhancement: wire shutdown ctx through.
type kindsExporterAdapter struct {
	name  string
	inner kinds.ExporterPlugin
}

func (k *kindsExporterAdapter) ExportMetrics(batch []*observability.Sample) error {
	out := make([]*kinds.MetricSample, len(batch))
	for i, s := range batch {
		out[i] = &kinds.MetricSample{
			Name:      s.Name,
			Type:      s.Type,
			Value:     s.Value,
			Labels:    s.Labels,
			Timestamp: s.Timestamp,
		}
	}
	return k.inner.ExportMetrics(context.Background(), out)
}

func (k *kindsExporterAdapter) ExportLogs(batch []*observability.LogRecord) error {
	out := make([]*kinds.LogRecord, len(batch))
	for i, r := range batch {
		out[i] = &kinds.LogRecord{
			Timestamp: r.Timestamp,
			Level:     r.Level,
			Message:   r.Message,
			Fields:    r.Fields,
		}
	}
	return k.inner.ExportLogs(context.Background(), out)
}

func (k *kindsExporterAdapter) ExportTraces(batch []*observability.Span) error {
	out := make([]*kinds.TraceSpan, len(batch))
	for i, sp := range batch {
		out[i] = &kinds.TraceSpan{
			TraceID:       sp.TraceID,
			SpanID:        sp.SpanID,
			ParentSpanID:  sp.ParentSpanID,
			Name:          sp.Name,
			StartUnixNano: sp.StartUnixNano,
			EndUnixNano:   sp.EndUnixNano,
			Attributes:    sp.Attributes,
		}
	}
	return k.inner.ExportTraces(context.Background(), out)
}

func (k *kindsExporterAdapter) Close() error { return k.inner.Close() }

// validateObservability checks that every name listed under
// `observability.exporters` resolves to a known plugin whose kind is
// "exporter". Called during boot before fan-out wiring.
func (s *Server) validateObservability() error {
	if s.Config.Observability == nil || len(s.Config.Observability.Exporters) == 0 {
		return nil
	}
	for _, name := range s.Config.Observability.Exporters {
		pc, ok := s.Config.Plugins[name]
		if !ok {
			return fmt.Errorf("observability.exporters: unknown plugin %q (not found in plugins:)", name)
		}
		if pc.Kind != "" && pc.Kind != plugins.KindExporter {
			return fmt.Errorf("observability.exporters: plugin %q has kind=%q, expected %q",
				name, pc.Kind, plugins.KindExporter)
		}
	}
	return nil
}

// bootstrapObservability wires Prometheus + plugin exporters under a
// single Tee published as observability.Default(). Safe to call even
// when no exporter plugins are configured — Tee with just the
// Prometheus sink keeps current behaviour.
func (s *Server) bootstrapObservability() error {
	if err := s.validateObservability(); err != nil {
		return err
	}

	// Always have a Prometheus sink so /metrics keeps working.
	prom := observability.NewPrometheusSink()

	// Subset selection: only plugins listed under observability.exporters
	// participate in fan-out. If none configured, all exporter-kind
	// plugins are wired (sensible default for "I configured an OTel
	// plugin and that's all I want").
	exporters := kinds.LoadExporter(plugins.Default())
	wanted := map[string]bool{}
	if s.Config.Observability != nil {
		for _, n := range s.Config.Observability.Exporters {
			wanted[n] = true
		}
	}

	adapters := map[string]observability.PluginExporter{}
	for name, p := range exporters {
		if len(wanted) > 0 && !wanted[name] {
			continue
		}
		adapters[name] = &kindsExporterAdapter{name: name, inner: p}
	}

	fanout := observability.NewFanout(adapters)
	s.fanout = fanout
	observability.SetDefault(observability.NewTee(prom, fanout))

	if len(adapters) > 0 {
		log.Printf("observability: wired %d exporter plugin(s)", len(adapters))
	}
	return nil
}
