package kinds

import (
	"context"
	"encoding/json"
	"testing"
)

func TestExporterAdapterMetrics(t *testing.T) {
	rc := &fakeRPC{resp: json.RawMessage(`null`)}
	a := &exporterAdapter{rpc: rc}
	if err := a.ExportMetrics(context.Background(),
		[]*MetricSample{{Name: "n", Type: "counter", Value: 1}}); err != nil {
		t.Fatal(err)
	}
	if rc.last.method != MethodExporterMetrics {
		t.Errorf("method = %s", rc.last.method)
	}
}

func TestExporterAdapterTraces(t *testing.T) {
	rc := &fakeRPC{resp: json.RawMessage(`null`)}
	a := &exporterAdapter{rpc: rc}
	if err := a.ExportTraces(context.Background(),
		[]*TraceSpan{{TraceID: "t", SpanID: "s", Name: "x"}}); err != nil {
		t.Fatal(err)
	}
	if rc.last.method != MethodExporterTraces {
		t.Errorf("method = %s", rc.last.method)
	}
}

func TestExporterAdapterLogs(t *testing.T) {
	rc := &fakeRPC{resp: json.RawMessage(`null`)}
	a := &exporterAdapter{rpc: rc}
	if err := a.ExportLogs(context.Background(),
		[]*LogRecord{{Level: "info", Message: "m"}}); err != nil {
		t.Fatal(err)
	}
	if rc.last.method != MethodExporterLogs {
		t.Errorf("method = %s", rc.last.method)
	}
}
