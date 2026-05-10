package http

import (
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

// CircuitBreaker is a simple three-state breaker (closed → open →
// half-open → closed). When wrapped around a handler:
//
//	closed:   normal operation; consecutive 5xx responses or panics
//	          increment a failure counter
//	open:     short-circuit with 503 Service Unavailable, no upstream
//	          call; resets after CooldownDuration
//	half-open: lets a single probe request through; if it succeeds we
//	          close again, on failure we re-open with a fresh cooldown
//
// Designed for upstream-fronting routes (forward / api / plugin) where
// you want to stop hammering a sick dependency. State is per-breaker,
// not per-request — pair one breaker per backend.
type CircuitBreaker struct {
	failureThreshold int
	cooldown         time.Duration

	// State machine. atomic.Int32 backs the public State() so callers
	// can read without a lock.
	state          atomic.Int32 // 0=closed, 1=open, 2=half-open
	failures       atomic.Int32
	openedAt       atomic.Int64 // unix nanos
	halfOpenInFlight atomic.Bool

	// metrics
	rejectsTotal atomic.Int64
	tripsTotal   atomic.Int64

	mu sync.Mutex // serializes transitions
}

const (
	stateClosed   int32 = 0
	stateOpen     int32 = 1
	stateHalfOpen int32 = 2
)

// NewCircuitBreaker creates a breaker that trips after `threshold`
// consecutive failures and stays open for `cooldown` before allowing a
// half-open probe. Both default to sane values when zero.
func NewCircuitBreaker(threshold int, cooldown time.Duration) *CircuitBreaker {
	if threshold <= 0 {
		threshold = 5
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &CircuitBreaker{failureThreshold: threshold, cooldown: cooldown}
}

// State returns "closed" | "open" | "half-open".
func (cb *CircuitBreaker) State() string {
	switch cb.state.Load() {
	case stateOpen:
		return "open"
	case stateHalfOpen:
		return "half-open"
	default:
		return "closed"
	}
}

// Stats exposes counters for /metrics + admin.
func (cb *CircuitBreaker) Stats() (state string, rejects, trips int64) {
	return cb.State(), cb.rejectsTotal.Load(), cb.tripsTotal.Load()
}

// Middleware wraps next with the breaker. The breaker counts a request
// as a failure if the wrapped handler writes a 5xx status code OR
// panics during ServeHTTP (panic is re-thrown after recording so the
// outer recover middleware / runtime can take over).
func (cb *CircuitBreaker) Middleware(next http.Handler) http.Handler {
	return cb.MiddlewareWithFail(next, nil)
}

// MiddlewareWithFail is Middleware with a custom rejection renderer
// (used by the orchestrator's `limits:` block). When onFail is nil,
// the built-in 503 + Retry-After response is used.
func (cb *CircuitBreaker) MiddlewareWithFail(next http.Handler, onFail http.HandlerFunc) http.Handler {
	reject := func(w http.ResponseWriter, r *http.Request, retryAfter string) {
		cb.rejectsTotal.Add(1)
		if onFail != nil {
			onFail(w, r)
			return
		}
		w.Header().Set("Retry-After", retryAfter)
		http.Error(w, "service temporarily unavailable", http.StatusServiceUnavailable)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Possibly transition open → half-open if cooldown has elapsed.
		cb.maybeHalfOpen()

		switch cb.state.Load() {
		case stateOpen:
			reject(w, r, durationSeconds(cb.cooldown))
			return
		case stateHalfOpen:
			// Only one probe allowed at a time; everyone else is rejected.
			if !cb.halfOpenInFlight.CompareAndSwap(false, true) {
				reject(w, r, "1")
				return
			}
			defer cb.halfOpenInFlight.Store(false)
		}

		sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}

		panicked := true
		defer func() {
			if panicked {
				cb.recordFailure()
				panic(recover())
			}
		}()
		next.ServeHTTP(sw, r)
		panicked = false

		if sw.status >= 500 {
			cb.recordFailure()
		} else {
			cb.recordSuccess()
		}
	})
}

func (cb *CircuitBreaker) recordSuccess() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	cb.failures.Store(0)
	if cb.state.Load() != stateClosed {
		cb.state.Store(stateClosed)
	}
}

func (cb *CircuitBreaker) recordFailure() {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	n := cb.failures.Add(1)
	if cb.state.Load() == stateHalfOpen {
		// Probe failed → re-open with fresh cooldown.
		cb.openedAt.Store(time.Now().UnixNano())
		cb.state.Store(stateOpen)
		cb.tripsTotal.Add(1)
		cb.failures.Store(0)
		return
	}
	if n >= int32(cb.failureThreshold) && cb.state.Load() == stateClosed {
		cb.openedAt.Store(time.Now().UnixNano())
		cb.state.Store(stateOpen)
		cb.tripsTotal.Add(1)
		cb.failures.Store(0)
	}
}

func (cb *CircuitBreaker) maybeHalfOpen() {
	if cb.state.Load() != stateOpen {
		return
	}
	if time.Since(time.Unix(0, cb.openedAt.Load())) < cb.cooldown {
		return
	}
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if cb.state.Load() == stateOpen &&
		time.Since(time.Unix(0, cb.openedAt.Load())) >= cb.cooldown {
		cb.state.Store(stateHalfOpen)
	}
}

// statusWriter mirrors the upstream status so the breaker can read it.
type statusWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (s *statusWriter) WriteHeader(c int) {
	if s.wroteHeader {
		return
	}
	s.wroteHeader = true
	s.status = c
	s.ResponseWriter.WriteHeader(c)
}

func (s *statusWriter) Write(b []byte) (int, error) {
	if !s.wroteHeader {
		s.wroteHeader = true
	}
	return s.ResponseWriter.Write(b)
}

func (s *statusWriter) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func durationSeconds(d time.Duration) string {
	s := int(d.Seconds())
	if s < 1 {
		s = 1
	}
	// avoid strconv import for this tiny case
	return itoa(s)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
