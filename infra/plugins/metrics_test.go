package plugins

import (
	"context"
	"errors"
	"testing"
	"time"
)

type fakeClientPM struct {
	delay time.Duration
	err   error
}

func (f *fakeClientPM) Call(_ context.Context, _ *Request) (*Response, error) {
	if f.delay > 0 {
		time.Sleep(f.delay)
	}
	if f.err != nil {
		return nil, f.err
	}
	return &Response{Status: 200}, nil
}
func (f *fakeClientPM) Close() error { return nil }

func TestInstrumentedClientCounters(t *testing.T) {
	c := wrapWithMetrics("unit_x", &fakeClientPM{}).(*instrumentedClient)
	for i := 0; i < 5; i++ {
		_, _ = c.Call(context.Background(), &Request{})
	}
	if c.CallCount() != 5 {
		t.Errorf("calls = %d", c.CallCount())
	}
	if c.ErrorCount() != 0 {
		t.Errorf("errors = %d", c.ErrorCount())
	}

	c2 := wrapWithMetrics("unit_y", &fakeClientPM{err: errors.New("boom")}).(*instrumentedClient)
	_, _ = c2.Call(context.Background(), &Request{})
	if c2.ErrorCount() != 1 {
		t.Errorf("errors = %d", c2.ErrorCount())
	}
}

func TestInstrumentedClientLatency(t *testing.T) {
	c := wrapWithMetrics("unit_z", &fakeClientPM{delay: 5 * time.Millisecond}).(*instrumentedClient)
	_, _ = c.Call(context.Background(), &Request{})
	if c.AverageLatency() < 4*time.Millisecond {
		t.Errorf("avg latency = %v", c.AverageLatency())
	}
}

func TestRegistryStats(t *testing.T) {
	reg, _ := NewRegistry(nil)
	InjectForTest(reg, "raw_fake", &fakeClientPM{})
	// Manually wrap one to confirm Stats only reports counters for
	// instrumented entries.
	InjectForTest(reg, "wrapped", wrapWithMetrics("wrapped", &fakeClientPM{}))
	_, _ = reg.clients["wrapped"].Call(context.Background(), &Request{})

	stats := reg.Stats()
	got := map[string]PluginStats{}
	for _, s := range stats {
		got[s.Name] = s
	}
	if got["wrapped"].Calls != 1 {
		t.Errorf("wrapped.calls = %d", got["wrapped"].Calls)
	}
	if got["raw_fake"].Calls != 0 {
		t.Errorf("raw_fake.calls = %d", got["raw_fake"].Calls)
	}
}
