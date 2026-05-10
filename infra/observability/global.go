package observability

import "sync/atomic"

// defaultSink is the process-global sink. atomic.Value lets readers
// (Emit on the request path) be lock-free.
var defaultSink atomic.Value // holds Sink

// SetDefault publishes the global Sink. Call this once during boot
// (after the plugin registry exists). Subsequent calls overwrite.
func SetDefault(s Sink) {
	if s == nil {
		s = noopSink{}
	}
	defaultSink.Store(sinkBox{s})
}

// Default returns the installed sink, or a no-op if none is set.
func Default() Sink {
	v := defaultSink.Load()
	if v == nil {
		return noopSink{}
	}
	return v.(sinkBox).s
}

// sinkBox wraps Sink so atomic.Value sees a concrete type even when
// callers pass different implementations (Tee, Fanout, …).
type sinkBox struct{ s Sink }

// Tee multiplexes Emit* calls to multiple sinks. Used in boot to
// fan-out to (Prometheus + plugin Fanout) under a single Default().
type Tee struct{ sinks []Sink }

// NewTee builds a Tee. Nil entries are skipped.
func NewTee(sinks ...Sink) *Tee {
	out := &Tee{}
	for _, s := range sinks {
		if s != nil {
			out.sinks = append(out.sinks, s)
		}
	}
	return out
}

func (t *Tee) EmitMetric(s *Sample) {
	for _, sk := range t.sinks {
		sk.EmitMetric(s)
	}
}
func (t *Tee) EmitLog(r *LogRecord) {
	for _, sk := range t.sinks {
		sk.EmitLog(r)
	}
}
func (t *Tee) EmitTrace(sp *Span) {
	for _, sk := range t.sinks {
		sk.EmitTrace(sp)
	}
}
