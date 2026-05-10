package kinds

import "context"

// JSON-RPC method names exposed by exporter-kind plugins.
const (
	MethodExporterMetrics = "exporter.metrics"
	MethodExporterTraces  = "exporter.traces"
	MethodExporterLogs    = "exporter.logs"
)

// ExporterPlugin is the typed interface for KindExporter plugins
// (OTel-style: telemetry sinks for metrics, traces, and logs).
type ExporterPlugin interface {
	ExportMetrics(ctx context.Context, batch []*MetricSample) error
	ExportTraces(ctx context.Context, batch []*TraceSpan) error
	ExportLogs(ctx context.Context, batch []*LogRecord) error
	Close() error
}

// MetricSample is one data point. Type is "counter" | "gauge" | "histogram".
type MetricSample struct {
	Name      string            `json:"name"`
	Type      string            `json:"type"`
	Value     float64           `json:"value"`
	Labels    map[string]string `json:"labels,omitempty"`
	Timestamp int64             `json:"ts,omitempty"` // unix ms
}

// TraceSpan is a single span, OTel-shaped.
type TraceSpan struct {
	TraceID       string            `json:"trace_id"`
	SpanID        string            `json:"span_id"`
	ParentSpanID  string            `json:"parent_span_id,omitempty"`
	Name          string            `json:"name"`
	StartUnixNano int64             `json:"start_unix_nano"`
	EndUnixNano   int64             `json:"end_unix_nano"`
	Attributes    map[string]string `json:"attributes,omitempty"`
}

// LogRecord is one structured log entry.
type LogRecord struct {
	Timestamp int64          `json:"ts"`
	Level     string         `json:"level"`
	Message   string         `json:"message"`
	Fields    map[string]any `json:"fields,omitempty"`
}
