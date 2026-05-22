package plugins

import (
	"context"
	"encoding/json"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/luowensheng/wave/infra/metrics"
	"github.com/luowensheng/wave/infra/observability"
)

// instrumentedClient decorates a Client with per-plugin call counters
// and latency tracking. Counters are atomic so multiple goroutines can
// share a single client without contention.
type instrumentedClient struct {
	name           string
	inner          Client
	calls          atomic.Int64
	errors         atomic.Int64
	latencyMicros  atomic.Int64 // running sum, used to derive an average
	latencyCount   atomic.Int64
}

func wrapWithMetrics(name string, inner Client) Client {
	c := &instrumentedClient{name: name, inner: inner}
	registerPluginMetrics(name, c)
	return c
}

func (c *instrumentedClient) Call(ctx context.Context, req *Request) (*Response, error) {
	c.calls.Add(1)
	start := time.Now()
	resp, err := c.inner.Call(ctx, req)
	c.latencyMicros.Add(time.Since(start).Microseconds())
	c.latencyCount.Add(1)
	if err != nil {
		c.errors.Add(1)
	}
	emitPluginCall(c.name, "call", err)
	return resp, err
}

func (c *instrumentedClient) Close() error { return c.inner.Close() }

// RPC forwards typed JSON-RPC calls to the wrapped client when it
// implements RPCClient, recording the call alongside Call() traffic.
func (c *instrumentedClient) RPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	rc, ok := c.inner.(RPCClient)
	if !ok {
		return nil, fmt.Errorf("plugin transport does not support RPC")
	}
	c.calls.Add(1)
	start := time.Now()
	res, err := rc.RPC(ctx, method, params)
	c.latencyMicros.Add(time.Since(start).Microseconds())
	c.latencyCount.Add(1)
	if err != nil {
		c.errors.Add(1)
	}
	emitPluginCall(c.name, method, err)
	return res, err
}

// emitPluginCall pushes a per-call counter Sample into the unified
// observability sink. Status is "ok" or "error".
func emitPluginCall(name, method string, err error) {
	status := "ok"
	if err != nil {
		status = "error"
	}
	observability.Default().EmitMetric(&observability.Sample{
		Name:  "wave_plugin_calls_total",
		Type:  "counter",
		Value: 1,
		Labels: map[string]string{
			"plugin": name,
			"method": method,
			"status": status,
		},
	})
}

// CallCount, ErrorCount, AverageLatency expose the running stats so the
// admin dashboard / tests can read them without scraping /metrics.
func (c *instrumentedClient) CallCount() int64  { return c.calls.Load() }
func (c *instrumentedClient) ErrorCount() int64 { return c.errors.Load() }
func (c *instrumentedClient) AverageLatency() time.Duration {
	n := c.latencyCount.Load()
	if n == 0 {
		return 0
	}
	return time.Duration(c.latencyMicros.Load()/n) * time.Microsecond
}

func registerPluginMetrics(name string, c *instrumentedClient) {
	prefix := "wave_plugin_" + sanitize(name)
	metrics.Register(prefix+"_calls_total",
		"Total plugin calls for "+name+".",
		metrics.NewGaugeFunc(func() int64 { return c.calls.Load() }))
	metrics.Register(prefix+"_errors_total",
		"Total plugin errors for "+name+".",
		metrics.NewGaugeFunc(func() int64 { return c.errors.Load() }))
	metrics.Register(prefix+"_latency_avg_micros",
		"Mean plugin latency in microseconds for "+name+".",
		metrics.NewGaugeFunc(func() int64 {
			n := c.latencyCount.Load()
			if n == 0 {
				return 0
			}
			return c.latencyMicros.Load() / n
		}))
}

// sanitize keeps Prometheus metric names valid: only letters/digits/_.
func sanitize(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out = append(out, byte(r))
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}

// PluginStats is a single plugin's snapshot, exposed for the admin
// dashboard. Returned only for instrumented clients (the path through
// NewRegistry); non-instrumented test fakes report zero.
type PluginStats struct {
	Name        string
	Calls       int64
	Errors      int64
	AvgLatency  time.Duration
}

// Stats lists every plugin in the registry with its counters.
func (r *Registry) Stats() []PluginStats {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]PluginStats, 0, len(r.clients))
	for name, c := range r.clients {
		ps := PluginStats{Name: name}
		if ic, ok := c.(*instrumentedClient); ok {
			ps.Calls = ic.CallCount()
			ps.Errors = ic.ErrorCount()
			ps.AvgLatency = ic.AverageLatency()
		}
		out = append(out, ps)
	}
	return out
}
