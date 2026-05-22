package observability

import (
	"bytes"
	"strings"
	"testing"

	"github.com/luowensheng/wave/infra/metrics"
)

func TestPrometheusSink_CounterEmits(t *testing.T) {
	metrics.Reset()
	p := NewPrometheusSink()
	p.EmitMetric(&Sample{
		Name:  "obs_test_counter",
		Type:  "counter",
		Value: 5,
	})
	p.EmitMetric(&Sample{
		Name:  "obs_test_counter",
		Type:  "counter",
		Value: 7,
	})
	var buf bytes.Buffer
	metrics.Render(&buf)
	if !strings.Contains(buf.String(), "obs_test_counter") {
		t.Fatalf("counter not exposed: %s", buf.String())
	}
}

func TestPrometheusSink_LabelsEncodedInName(t *testing.T) {
	metrics.Reset()
	p := NewPrometheusSink()
	p.EmitMetric(&Sample{
		Name:   "obs_test_labelled",
		Type:   "counter",
		Value:  1,
		Labels: map[string]string{"route": "/foo", "method": "GET"},
	})
	var buf bytes.Buffer
	metrics.Render(&buf)
	out := buf.String()
	if !strings.Contains(out, "obs_test_labelled_method_GET_route__foo") {
		t.Fatalf("missing labelled metric in: %s", out)
	}
}

func TestPrometheusSink_GaugeAndHistogram(t *testing.T) {
	metrics.Reset()
	p := NewPrometheusSink()
	p.EmitMetric(&Sample{Name: "g", Type: "gauge", Value: 3})
	p.EmitMetric(&Sample{Name: "h", Type: "histogram", Value: 0.123})
	var buf bytes.Buffer
	metrics.Render(&buf)
	out := buf.String()
	for _, want := range []string{"h_count", "h_last", "g "} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestPrometheusSink_LogTraceAreNoops(t *testing.T) {
	metrics.Reset()
	p := NewPrometheusSink()
	p.EmitLog(&LogRecord{Message: "x"})
	p.EmitTrace(&Span{Name: "x"})
	var buf bytes.Buffer
	metrics.Render(&buf)
	if buf.Len() != 0 {
		t.Fatalf("log/trace should not emit metrics: %s", buf.String())
	}
}
