// Package metrics is a tiny zero-dependency Prometheus text-format
// exposer. It keeps a few global counters/gauges and renders them on
// demand, so we get useful observability without pulling in
// prometheus/client_golang. Drop in a real client later if you need
// histograms or label cardinality.
package metrics

import (
	"fmt"
	"io"
	"sort"
	"sync"
	"sync/atomic"
)

type metric interface {
	render(w io.Writer, name, help string)
}

type Counter struct{ v atomic.Int64 }

func (c *Counter) Inc()              { c.v.Add(1) }
func (c *Counter) Add(n int64)       { c.v.Add(n) }
func (c *Counter) Get() int64        { return c.v.Load() }
func (c *Counter) render(w io.Writer, name, help string) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s counter\n%s %d\n", name, help, name, name, c.v.Load())
}

// GaugeFunc reports the current value of an externally-owned gauge
// (e.g., subscriber count from a broker) on each scrape.
type GaugeFunc struct {
	fn func() int64
}

func NewGaugeFunc(fn func() int64) *GaugeFunc { return &GaugeFunc{fn: fn} }
func (g *GaugeFunc) render(w io.Writer, name, help string) {
	fmt.Fprintf(w, "# HELP %s %s\n# TYPE %s gauge\n%s %d\n", name, help, name, name, g.fn())
}

type entry struct {
	name string
	help string
	m    metric
}

var (
	mu      sync.RWMutex
	entries = []entry{}
	byName  = map[string]*entry{}
)

// Register installs a metric. Idempotent on name: re-registration with the
// same name is a no-op (returns the existing metric so callers can wire
// to a single instance).
func Register(name, help string, m metric) {
	mu.Lock()
	defer mu.Unlock()
	if _, ok := byName[name]; ok {
		return
	}
	entries = append(entries, entry{name: name, help: help, m: m})
	byName[name] = &entries[len(entries)-1]
}

// Render writes the entire metric set in Prometheus text exposition format.
func Render(w io.Writer) {
	mu.RLock()
	defer mu.RUnlock()
	// Stable ordering so scrapes diff cleanly.
	idxs := make([]int, len(entries))
	for i := range entries {
		idxs[i] = i
	}
	sort.Slice(idxs, func(i, j int) bool { return entries[idxs[i]].name < entries[idxs[j]].name })
	for _, i := range idxs {
		e := entries[i]
		e.m.render(w, e.name, e.help)
	}
}

// Reset is intended for tests only; clears the global registry.
func Reset() {
	mu.Lock()
	defer mu.Unlock()
	entries = nil
	byName = map[string]*entry{}
}
