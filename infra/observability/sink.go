// Package observability is the unified push-style telemetry surface
// for the orchestrator. Code that emits metrics, traces, or logs goes
// through Default() — a Sink that fans out to (a) the in-process
// Prometheus exposer (infra/metrics) and (b) any number of plugin
// exporter subscribers (OTel, Datadog, etc).
//
// The package keeps zero deps on infra/plugins to avoid an import
// cycle (orchestrator → infra/observability → infra/plugins → …).
// Plugin exporters are wired in via the local PluginExporter
// interface; the orchestrator supplies an adapter from
// kinds.ExporterPlugin.
package observability

// Sink is the unified push surface that the orchestrator emits to.
// All Emit* calls MUST be non-blocking; implementations are expected
// to drop on overflow rather than block the request path.
type Sink interface {
	EmitMetric(sample *Sample)
	EmitLog(record *LogRecord)
	EmitTrace(span *Span)
}

// Sample is one metric data point. Type is "counter" | "gauge" |
// "histogram"; the Prometheus sink degrades histograms to a counter
// (count) plus a gauge (last-value) for v1 — see prometheus_sink.go.
type Sample struct {
	Name      string
	Type      string
	Value     float64
	Labels    map[string]string
	Timestamp int64 // unix ms
}

// LogRecord mirrors kinds.LogRecord but lives here so emitters don't
// import infra/plugins.
type LogRecord struct {
	Timestamp int64
	Level     string
	Message   string
	Fields    map[string]any
}

// Span mirrors kinds.TraceSpan.
type Span struct {
	TraceID       string
	SpanID        string
	ParentSpanID  string
	Name          string
	StartUnixNano int64
	EndUnixNano   int64
	Attributes    map[string]string
}

// PluginExporter is the local-to-this-package shape that Fanout
// drains into. The orchestrator supplies an adapter from
// kinds.ExporterPlugin → PluginExporter.
type PluginExporter interface {
	ExportMetrics(batch []*Sample) error
	ExportLogs(batch []*LogRecord) error
	ExportTraces(batch []*Span) error
	Close() error
}

// noopSink is the safe default returned by Default() before
// SetDefault is called. Drops everything.
type noopSink struct{}

func (noopSink) EmitMetric(*Sample)    {}
func (noopSink) EmitLog(*LogRecord)    {}
func (noopSink) EmitTrace(*Span)       {}
