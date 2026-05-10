package observability

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeExporter records every batch it sees. Optional sleep simulates
// a slow subscriber; optional panic exercises the recover path.
type fakeExporter struct {
	mu       sync.Mutex
	metrics  [][]*Sample
	logs     [][]*LogRecord
	traces   [][]*Span
	closed   atomic.Bool
	sleep    time.Duration
	panicNow atomic.Bool
}

func (f *fakeExporter) ExportMetrics(b []*Sample) error {
	if f.panicNow.Load() {
		panic("boom")
	}
	if f.sleep > 0 {
		time.Sleep(f.sleep)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]*Sample, len(b))
	copy(cp, b)
	f.metrics = append(f.metrics, cp)
	return nil
}
func (f *fakeExporter) ExportLogs(b []*LogRecord) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]*LogRecord, len(b))
	copy(cp, b)
	f.logs = append(f.logs, cp)
	return nil
}
func (f *fakeExporter) ExportTraces(b []*Span) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	cp := make([]*Span, len(b))
	copy(cp, b)
	f.traces = append(f.traces, cp)
	return nil
}
func (f *fakeExporter) Close() error { f.closed.Store(true); return nil }

func (f *fakeExporter) totalMetrics() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, b := range f.metrics {
		n += len(b)
	}
	return n
}

func TestFanout_BatchSizeFlush(t *testing.T) {
	fe := &fakeExporter{}
	f := NewFanout(map[string]PluginExporter{"x": fe},
		WithBatchSize(3), WithFlushPeriod(time.Hour))
	defer f.Close()

	for i := 0; i < 6; i++ {
		f.EmitMetric(&Sample{Name: "m", Type: "counter", Value: 1})
	}
	// Wait for two batches of 3 to flush.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fe.totalMetrics() >= 6 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := fe.totalMetrics(); got != 6 {
		t.Fatalf("expected 6 metrics across batches, got %d", got)
	}
}

func TestFanout_TimeFlush(t *testing.T) {
	fe := &fakeExporter{}
	f := NewFanout(map[string]PluginExporter{"x": fe},
		WithBatchSize(1024), WithFlushPeriod(50*time.Millisecond))
	defer f.Close()

	f.EmitMetric(&Sample{Name: "m", Type: "counter", Value: 1})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if fe.totalMetrics() >= 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := fe.totalMetrics(); got != 1 {
		t.Fatalf("expected time-based flush of 1, got %d", got)
	}
}

func TestFanout_SlowSubscriberDrops(t *testing.T) {
	slow := &fakeExporter{sleep: 200 * time.Millisecond}
	f := NewFanout(map[string]PluginExporter{"slow": slow},
		WithBatchSize(1), WithFlushPeriod(time.Hour))
	defer f.Close()

	// Emit way more than the channel buffer; some must drop.
	for i := 0; i < defaultChannelBuffer*2; i++ {
		f.EmitMetric(&Sample{Name: "m", Type: "counter", Value: 1})
	}
	// Inspect drop counter — slow channel should have overflowed.
	drops := f.SubscriberDrops()
	if drops["slow"] == 0 {
		t.Fatalf("expected drops, got %v", drops)
	}
}

func TestFanout_CloseDrains(t *testing.T) {
	fe := &fakeExporter{}
	f := NewFanout(map[string]PluginExporter{"x": fe},
		WithBatchSize(1024), WithFlushPeriod(time.Hour))

	for i := 0; i < 10; i++ {
		f.EmitMetric(&Sample{Name: "m", Type: "counter", Value: 1})
	}
	// Without close we'd never see them (huge batch + huge interval).
	if err := f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	if got := fe.totalMetrics(); got != 10 {
		t.Fatalf("expected drain of 10 on Close, got %d", got)
	}
	if !fe.closed.Load() {
		t.Fatal("subscriber sink not closed")
	}
}

func TestFanout_PanicIsolated(t *testing.T) {
	bad := &fakeExporter{}
	bad.panicNow.Store(true)
	good := &fakeExporter{}
	f := NewFanout(map[string]PluginExporter{"bad": bad, "good": good},
		WithBatchSize(1), WithFlushPeriod(10*time.Millisecond))
	defer f.Close()

	for i := 0; i < 5; i++ {
		f.EmitMetric(&Sample{Name: "m", Type: "counter", Value: 1})
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if good.totalMetrics() >= 5 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if good.totalMetrics() < 5 {
		t.Fatalf("good subscriber starved: got %d", good.totalMetrics())
	}
}

func TestFanout_NoSubscribers(t *testing.T) {
	f := NewFanout(nil)
	defer f.Close()
	// Must not block / panic.
	f.EmitMetric(&Sample{Name: "x"})
	f.EmitLog(&LogRecord{Message: "x"})
	f.EmitTrace(&Span{Name: "x"})
}

// errExporter always returns an error; verifies error path doesn't crash.
type errExporter struct{}

func (errExporter) ExportMetrics(_ []*Sample) error { return errors.New("nope") }
func (errExporter) ExportLogs(_ []*LogRecord) error { return errors.New("nope") }
func (errExporter) ExportTraces(_ []*Span) error    { return errors.New("nope") }
func (errExporter) Close() error                    { return nil }

func TestFanout_ErrorIsLoggedNotFatal(t *testing.T) {
	f := NewFanout(map[string]PluginExporter{"err": errExporter{}},
		WithBatchSize(1), WithFlushPeriod(10*time.Millisecond))
	defer f.Close()
	f.EmitMetric(&Sample{Name: "m"})
	time.Sleep(50 * time.Millisecond)
}
