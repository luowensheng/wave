package metrics

import (
	"bytes"
	"strings"
	"testing"
)

func TestCounterAndGauge(t *testing.T) {
	Reset()
	c := &Counter{}
	c.Add(3)
	c.Inc()
	Register("test_counter", "test help", c)

	gv := int64(42)
	g := NewGaugeFunc(func() int64 { return gv })
	Register("test_gauge", "g help", g)

	var buf bytes.Buffer
	Render(&buf)
	out := buf.String()
	if !strings.Contains(out, "test_counter 4") {
		t.Errorf("counter missing/wrong: %s", out)
	}
	if !strings.Contains(out, "test_gauge 42") {
		t.Errorf("gauge missing: %s", out)
	}
	if !strings.Contains(out, "# TYPE test_counter counter") {
		t.Errorf("missing TYPE line: %s", out)
	}
}

func TestRegisterIdempotent(t *testing.T) {
	Reset()
	Register("x", "h", &Counter{})
	Register("x", "h", &Counter{})
	var buf bytes.Buffer
	Render(&buf)
	if strings.Count(buf.String(), "# HELP x ") != 1 {
		t.Errorf("re-registration should be a no-op: %s", buf.String())
	}
}
