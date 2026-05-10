package connections

import (
	"sync"
	"sync/atomic"
)

// Broker fans events out from a single Publish() to N Subscribe()s.
// Slow subscribers do NOT block the publisher: events are dropped onto
// the floor and counted, and chronically-slow subs are eventually closed.
type Broker struct {
	cfg *ConnectionConfig

	mu          sync.RWMutex
	subscribers map[string]*subscriber
	ring        *ringBuffer

	// Metrics — read with atomic.LoadInt64.
	publishedTotal atomic.Int64
	droppedTotal   atomic.Int64
}

type subscriber struct {
	id      string
	ch      chan []byte
	dropped atomic.Int64
}

// NewBroker constructs a broker from a ConnectionConfig.
func NewBroker(cfg *ConnectionConfig) *Broker {
	return &Broker{
		cfg:         cfg,
		subscribers: make(map[string]*subscriber),
		ring:        newRingBuffer(cfg.bufferSize()),
	}
}

// Subscribe registers a new client. Returns the channel of events plus
// an unsubscribe func that the caller MUST call (defer is good) so the
// broker doesn't leak goroutines / channels.
//
// The second return is false if MaxClients is reached.
func (b *Broker) Subscribe(id string) (<-chan []byte, func(), bool) {
	b.mu.Lock()
	if len(b.subscribers) >= b.cfg.maxClients() {
		b.mu.Unlock()
		return nil, func() {}, false
	}
	sub := &subscriber{
		id: id,
		ch: make(chan []byte, b.cfg.bufferSize()),
	}
	b.subscribers[id] = sub

	// Replay buffered events to the late joiner (under lock so we don't race
	// with Publish into the same channel).
	for _, e := range b.ring.snapshot() {
		select {
		case sub.ch <- e:
		default:
		}
	}
	b.mu.Unlock()

	cancel := func() {
		b.mu.Lock()
		if cur, ok := b.subscribers[id]; ok && cur == sub {
			delete(b.subscribers, id)
			close(sub.ch)
		}
		b.mu.Unlock()
	}
	return sub.ch, cancel, true
}

// Publish fans an event out to every current subscriber. Non-blocking;
// the per-subscriber send is a `select default` so a slow client only
// loses events, never holds up the publisher.
func (b *Broker) Publish(event []byte) {
	b.publishedTotal.Add(1)

	b.mu.RLock()
	defer b.mu.RUnlock()
	b.ring.push(event)
	for _, sub := range b.subscribers {
		select {
		case sub.ch <- event:
		default:
			sub.dropped.Add(1)
			b.droppedTotal.Add(1)
		}
	}
}

// SubscriberCount returns the current number of connected subscribers.
func (b *Broker) SubscriberCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.subscribers)
}

// Stats returns per-broker counters; useful for /metrics later.
func (b *Broker) Stats() (subs int, published, dropped int64) {
	return b.SubscriberCount(), b.publishedTotal.Load(), b.droppedTotal.Load()
}

// Config returns the underlying connection config (read-only access).
func (b *Broker) Config() *ConnectionConfig { return b.cfg }

// ── ring buffer ───────────────────────────────────────────────────────────

type ringBuffer struct {
	buf  [][]byte
	pos  int
	full bool
}

func newRingBuffer(n int) *ringBuffer {
	if n < 1 {
		n = 1
	}
	return &ringBuffer{buf: make([][]byte, n)}
}

func (r *ringBuffer) push(b []byte) {
	r.buf[r.pos] = b
	r.pos++
	if r.pos >= len(r.buf) {
		r.pos = 0
		r.full = true
	}
}

func (r *ringBuffer) snapshot() [][]byte {
	if !r.full {
		out := make([][]byte, r.pos)
		copy(out, r.buf[:r.pos])
		return out
	}
	out := make([][]byte, 0, len(r.buf))
	out = append(out, r.buf[r.pos:]...)
	out = append(out, r.buf[:r.pos]...)
	return out
}
