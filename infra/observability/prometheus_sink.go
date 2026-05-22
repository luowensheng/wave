package observability

import (
	"sort"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/luowensheng/wave/infra/metrics"
)

// PrometheusSink adapts incoming Sample/Log/Trace events into the
// in-process Prometheus exposer (infra/metrics). Counters auto-register
// on first use; gauges hold the most-recent value per (name +
// label-fingerprint).
//
// Histograms in v1: degrade to a Counter on the request count + a
// Gauge on the last-observed value. A real bucketed histogram needs
// cumulative buckets in the Prom text format and is deferred.
type PrometheusSink struct {
	mu       sync.Mutex
	counters map[string]*atomicCounter
	gauges   map[string]*atomicGauge
}

// NewPrometheusSink builds an empty sink. Metrics auto-register the
// first time they're seen.
func NewPrometheusSink() *PrometheusSink {
	return &PrometheusSink{
		counters: map[string]*atomicCounter{},
		gauges:   map[string]*atomicGauge{},
	}
}

// EmitMetric routes by Type. Logs/Traces are dropped (the Prometheus
// exposer is metrics-only).
func (p *PrometheusSink) EmitMetric(s *Sample) {
	if s == nil || s.Name == "" {
		return
	}
	key := metricKey(s.Name, s.Labels)
	switch s.Type {
	case "counter":
		c := p.getOrCreateCounter(key, s.Name, s.Labels)
		c.add(s.Value)
	case "gauge":
		g := p.getOrCreateGauge(key, s.Name, s.Labels)
		g.set(s.Value)
	case "histogram":
		// Degrade: count + last-value gauge.
		ck := key + "|count"
		c := p.getOrCreateCounter(ck, s.Name+"_count", s.Labels)
		c.add(1)
		gk := key + "|last"
		g := p.getOrCreateGauge(gk, s.Name+"_last", s.Labels)
		g.set(s.Value)
	default:
		// Unknown type — treat as gauge for forward-compat.
		g := p.getOrCreateGauge(key, s.Name, s.Labels)
		g.set(s.Value)
	}
}

func (p *PrometheusSink) EmitLog(*LogRecord) {}
func (p *PrometheusSink) EmitTrace(*Span)    {}

func (p *PrometheusSink) getOrCreateCounter(key, name string, labels map[string]string) *atomicCounter {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.counters[key]; ok {
		return c
	}
	c := &atomicCounter{}
	promName := promMetricName(name, labels)
	metrics.Register(promName,
		"Auto-registered from observability.Sample.",
		metrics.NewGaugeFunc(func() int64 { return c.intValue() }))
	p.counters[key] = c
	return c
}

func (p *PrometheusSink) getOrCreateGauge(key, name string, labels map[string]string) *atomicGauge {
	p.mu.Lock()
	defer p.mu.Unlock()
	if g, ok := p.gauges[key]; ok {
		return g
	}
	g := &atomicGauge{}
	promName := promMetricName(name, labels)
	metrics.Register(promName,
		"Auto-registered from observability.Sample.",
		metrics.NewGaugeFunc(func() int64 { return g.intValue() }))
	p.gauges[key] = g
	return g
}

// metricKey is a stable string identity for (name, label-set) used as
// a map key by the sink internally.
func metricKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(name)
	for _, k := range keys {
		b.WriteByte('|')
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(labels[k])
	}
	return b.String()
}

// promMetricName encodes labels into the metric name itself because
// the zero-dep exposer doesn't support Prometheus label syntax. Shape:
// `<name>_<k1>_<v1>_<k2>_<v2>` with non-alnum sanitised to `_`.
func promMetricName(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return sanitizeProm(name)
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString(sanitizeProm(name))
	for _, k := range keys {
		b.WriteByte('_')
		b.WriteString(sanitizeProm(k))
		b.WriteByte('_')
		b.WriteString(sanitizeProm(labels[k]))
	}
	return b.String()
}

func sanitizeProm(s string) string {
	out := make([]byte, 0, len(s))
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			out = append(out, byte(r))
		} else {
			out = append(out, '_')
		}
	}
	return string(out)
}

// atomicCounter is a float-valued counter using int64 micro-units so
// we can present an int back to the existing GaugeFunc API. We store
// floor(value * 1e6) to keep two-decimal precision typical of seconds.
type atomicCounter struct{ v atomic.Int64 }

func (c *atomicCounter) add(delta float64) {
	c.v.Add(int64(delta * 1e6))
}
func (c *atomicCounter) intValue() int64 { return c.v.Load() / 1e6 }

type atomicGauge struct{ v atomic.Int64 }

func (g *atomicGauge) set(value float64) { g.v.Store(int64(value * 1e6)) }
func (g *atomicGauge) intValue() int64   { return g.v.Load() / 1e6 }
