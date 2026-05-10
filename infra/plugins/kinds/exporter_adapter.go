package kinds

import "context"

// exporterAdapter turns the JSON-RPC RPCClient into a typed ExporterPlugin.
type exporterAdapter struct {
	rpc  RPCClient
	name string
}

func (e *exporterAdapter) ExportMetrics(ctx context.Context, batch []*MetricSample) error {
	return rpcCall(ctx, e.rpc, MethodExporterMetrics,
		map[string]any{"samples": batch}, nil)
}

func (e *exporterAdapter) ExportTraces(ctx context.Context, batch []*TraceSpan) error {
	return rpcCall(ctx, e.rpc, MethodExporterTraces,
		map[string]any{"spans": batch}, nil)
}

func (e *exporterAdapter) ExportLogs(ctx context.Context, batch []*LogRecord) error {
	return rpcCall(ctx, e.rpc, MethodExporterLogs,
		map[string]any{"records": batch}, nil)
}

func (e *exporterAdapter) Close() error { return e.rpc.Close() }
