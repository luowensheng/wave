package observability

import (
	"sync/atomic"
	"testing"
)

type countingSink struct {
	metrics atomic.Int64
	logs    atomic.Int64
	traces  atomic.Int64
}

func (c *countingSink) EmitMetric(*Sample)    { c.metrics.Add(1) }
func (c *countingSink) EmitLog(*LogRecord)    { c.logs.Add(1) }
func (c *countingSink) EmitTrace(*Span)       { c.traces.Add(1) }

func TestTee_FansToAll(t *testing.T) {
	a, b := &countingSink{}, &countingSink{}
	tee := NewTee(a, nil, b)
	tee.EmitMetric(&Sample{})
	tee.EmitMetric(&Sample{})
	tee.EmitLog(&LogRecord{})
	tee.EmitTrace(&Span{})
	for _, s := range []*countingSink{a, b} {
		if s.metrics.Load() != 2 || s.logs.Load() != 1 || s.traces.Load() != 1 {
			t.Fatalf("counts: m=%d l=%d t=%d", s.metrics.Load(), s.logs.Load(), s.traces.Load())
		}
	}
}

func TestDefault_NoopUntilSet(t *testing.T) {
	// Reset by setting a fresh noop. Can't actually unset; just verify
	// Default returns a working sink.
	SetDefault(noopSink{})
	Default().EmitMetric(&Sample{})
	Default().EmitLog(&LogRecord{})
	Default().EmitTrace(&Span{})

	c := &countingSink{}
	SetDefault(c)
	Default().EmitMetric(&Sample{})
	if c.metrics.Load() != 1 {
		t.Fatalf("expected 1, got %d", c.metrics.Load())
	}
	// Reset for downstream tests.
	SetDefault(noopSink{})
}
