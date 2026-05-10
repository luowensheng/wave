package servers

import (
	"net/http"
	"sync/atomic"

	"wave/infra/connections"
	"wave/infra/metrics"
)

// Process-wide HTTP request counter — incremented inside the outer
// middleware in Start(). Cheap and label-free.
var requestsTotal atomic.Int64

func init() {
	metrics.Register("wave_requests_total",
		"Total HTTP requests served (post graceful-shutdown drain).",
		(&metrics.Counter{}))
	// We can't pass our atomic into the Counter directly, so wrap it as a
	// gauge-of-counter. Cleaner than smuggling state.
	metrics.Register("wave_requests_total_gauge",
		"Total HTTP requests served (raw).",
		metrics.NewGaugeFunc(func() int64 { return requestsTotal.Load() }))
}

// registerMetricsEndpoint installs GET /metrics and lazily attaches
// per-broker subscriber gauges + per-broker published/dropped counters
// for any connections currently registered.
func (s *Server) registerMetricsEndpoint() {
	if reg := connections.Default(); reg != nil {
		for name, broker := range reg.All() {
			n := name
			b := broker
			metrics.Register(
				metricName("wave_connection_subscribers", n),
				"Current SSE/WS subscriber count for connection "+n+".",
				metrics.NewGaugeFunc(func() int64 {
					subs, _, _ := b.Stats()
					return int64(subs)
				}),
			)
			metrics.Register(
				metricName("wave_connection_published_total", n),
				"Total events published to connection "+n+".",
				metrics.NewGaugeFunc(func() int64 {
					_, p, _ := b.Stats()
					return p
				}),
			)
			metrics.Register(
				metricName("wave_connection_dropped_total", n),
				"Total events dropped due to slow subscribers on connection "+n+".",
				metrics.NewGaugeFunc(func() int64 {
					_, _, d := b.Stats()
					return d
				}),
			)
		}
	}

	s.mux.HandleFunc("GET /metrics", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		metrics.Render(w)
	})
}

// metricName produces a Prometheus-safe `<base>{name="<sanitized>"}` style
// suffix. We keep the metric name a single identifier (no labels) for the
// zero-dep exposer; sanitize the connection name into the metric name.
func metricName(base, name string) string {
	out := []byte(base + "_")
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') {
			out = append(out, byte(r))
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}
