package observability

import (
	"log"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// Defaults for Fanout subscriber channels and batching.
const (
	defaultBatchSize     = 512
	defaultFlushPeriod   = time.Second
	defaultChannelBuffer = 4096
)

// Fanout is the multi-subscriber Sink. EmitMetric/Log/Trace are
// non-blocking — items are dropped into per-subscriber bounded
// channels and a worker per subscriber drains in batches.
type Fanout struct {
	subs        []*subscriber
	batchSize   int
	flushPeriod time.Duration
	wg          sync.WaitGroup
	closeOnce   sync.Once
	closed      atomic.Bool
}

type subscriber struct {
	name    string
	sink    PluginExporter
	metrics chan *Sample
	logs    chan *LogRecord
	traces  chan *Span
	stop    chan struct{}
	dropped atomic.Int64
}

// Dropped reports total items that this subscriber dropped because
// its inbound channel was full.
func (s *subscriber) Dropped() int64 { return s.dropped.Load() }

// FanoutOption customises Fanout construction.
type FanoutOption func(*Fanout)

// WithBatchSize overrides the default batch size (512).
func WithBatchSize(n int) FanoutOption {
	return func(f *Fanout) {
		if n > 0 {
			f.batchSize = n
		}
	}
}

// WithFlushPeriod overrides the default flush period (1s).
func WithFlushPeriod(d time.Duration) FanoutOption {
	return func(f *Fanout) {
		if d > 0 {
			f.flushPeriod = d
		}
	}
}

// NewFanout wires the supplied plugin exporters as subscribers and
// starts one drain goroutine per subscriber.
func NewFanout(plugins map[string]PluginExporter, opts ...FanoutOption) *Fanout {
	f := &Fanout{
		batchSize:   defaultBatchSize,
		flushPeriod: defaultFlushPeriod,
	}
	for _, o := range opts {
		o(f)
	}
	for name, p := range plugins {
		if p == nil {
			continue
		}
		s := &subscriber{
			name:    name,
			sink:    p,
			metrics: make(chan *Sample, defaultChannelBuffer),
			logs:    make(chan *LogRecord, defaultChannelBuffer),
			traces:  make(chan *Span, defaultChannelBuffer),
			stop:    make(chan struct{}),
		}
		f.subs = append(f.subs, s)
		f.wg.Add(1)
		go f.run(s)
	}
	return f
}

// EmitMetric drops the sample into every subscriber's metrics
// channel. Full channel = dropped, dropped counter incremented.
func (f *Fanout) EmitMetric(s *Sample) {
	if s == nil || f.closed.Load() {
		return
	}
	for _, sub := range f.subs {
		select {
		case sub.metrics <- s:
		default:
			sub.dropped.Add(1)
		}
	}
}

// EmitLog — see EmitMetric.
func (f *Fanout) EmitLog(r *LogRecord) {
	if r == nil || f.closed.Load() {
		return
	}
	for _, sub := range f.subs {
		select {
		case sub.logs <- r:
		default:
			sub.dropped.Add(1)
		}
	}
}

// EmitTrace — see EmitMetric.
func (f *Fanout) EmitTrace(sp *Span) {
	if sp == nil || f.closed.Load() {
		return
	}
	for _, sub := range f.subs {
		select {
		case sub.traces <- sp:
		default:
			sub.dropped.Add(1)
		}
	}
}

// SubscriberDrops returns a name→dropped-count snapshot, for the
// admin dashboard / metrics.
func (f *Fanout) SubscriberDrops() map[string]int64 {
	out := make(map[string]int64, len(f.subs))
	for _, s := range f.subs {
		out[s.name] = s.dropped.Load()
	}
	return out
}

// Close marks the fanout closed, drains in-flight per-subscriber
// batches, stops the workers, and calls Close on each subscriber.
// Idempotent.
func (f *Fanout) Close() error {
	f.closeOnce.Do(func() {
		f.closed.Store(true)
		for _, s := range f.subs {
			close(s.stop)
		}
		f.wg.Wait()
		for _, s := range f.subs {
			_ = safeClose(s.sink)
		}
	})
	return nil
}

func safeClose(p PluginExporter) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = nil
		}
	}()
	return p.Close()
}

// run is the per-subscriber drain loop. It batches up to batchSize
// items or flushPeriod, whichever first. A panic inside a sink call
// is logged and swallowed so it can't take down sibling drains.
func (f *Fanout) run(s *subscriber) {
	defer f.wg.Done()
	tick := time.NewTicker(f.flushPeriod)
	defer tick.Stop()

	var (
		mBuf []*Sample
		lBuf []*LogRecord
		tBuf []*Span
	)
	mBuf = make([]*Sample, 0, f.batchSize)
	lBuf = make([]*LogRecord, 0, f.batchSize)
	tBuf = make([]*Span, 0, f.batchSize)

	flushAll := func() {
		if len(mBuf) > 0 {
			callMetrics(s, mBuf)
			mBuf = mBuf[:0]
		}
		if len(lBuf) > 0 {
			callLogs(s, lBuf)
			lBuf = lBuf[:0]
		}
		if len(tBuf) > 0 {
			callTraces(s, tBuf)
			tBuf = tBuf[:0]
		}
	}

	for {
		select {
		case m := <-s.metrics:
			mBuf = append(mBuf, m)
			if len(mBuf) >= f.batchSize {
				callMetrics(s, mBuf)
				mBuf = mBuf[:0]
			}
		case r := <-s.logs:
			lBuf = append(lBuf, r)
			if len(lBuf) >= f.batchSize {
				callLogs(s, lBuf)
				lBuf = lBuf[:0]
			}
		case sp := <-s.traces:
			tBuf = append(tBuf, sp)
			if len(tBuf) >= f.batchSize {
				callTraces(s, tBuf)
				tBuf = tBuf[:0]
			}
		case <-tick.C:
			flushAll()
		case <-s.stop:
			// drain remaining buffered items off the channels too.
			drainChan(s.metrics, &mBuf)
			drainChan(s.logs, &lBuf)
			drainChan(s.traces, &tBuf)
			flushAll()
			return
		}
	}
}

func drainChan[T any](ch <-chan T, buf *[]T) {
	for {
		select {
		case v := <-ch:
			*buf = append(*buf, v)
		default:
			return
		}
	}
}

func callMetrics(s *subscriber, batch []*Sample) {
	defer recoverSubscriberPanic(s, "metrics")
	if err := s.sink.ExportMetrics(batch); err != nil {
		log.Printf("observability: subscriber=%s ExportMetrics err: %v", s.name, err)
	}
}

func callLogs(s *subscriber, batch []*LogRecord) {
	defer recoverSubscriberPanic(s, "logs")
	if err := s.sink.ExportLogs(batch); err != nil {
		log.Printf("observability: subscriber=%s ExportLogs err: %v", s.name, err)
	}
}

func callTraces(s *subscriber, batch []*Span) {
	defer recoverSubscriberPanic(s, "traces")
	if err := s.sink.ExportTraces(batch); err != nil {
		log.Printf("observability: subscriber=%s ExportTraces err: %v", s.name, err)
	}
}

func recoverSubscriberPanic(s *subscriber, kind string) {
	if r := recover(); r != nil {
		log.Printf("observability: subscriber=%s panic in %s: %v\n%s",
			s.name, kind, r, debug.Stack())
	}
}
