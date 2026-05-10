// Package errreport is a tiny pluggable error-reporting layer (the
// shape Sentry / Datadog SDKs expose). The point isn't to ship an SDK
// — it's to give every panic, plugin failure, audit-emit failure, etc.
// a single Capture() call site so a real backend can plug in later
// without touching every error site.
//
// Default reporter is a stderr logger; users register their own (real
// SDK adapter) at boot via SetDefault.
package errreport

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"runtime/debug"
	"sync"
)

// Severity ranks events for downstream triage. Mirrors Sentry levels.
type Severity string

const (
	SevDebug   Severity = "debug"
	SevInfo    Severity = "info"
	SevWarning Severity = "warning"
	SevError   Severity = "error"
	SevFatal   Severity = "fatal"
)

// Event is a single report. Keep keys snake_case for downstream JSON
// pipelines.
type Event struct {
	Severity   Severity
	Message    string
	Err        error
	Stack      string            // optional; populated for panics
	Tags       map[string]string // request_id, route, etc.
	HTTPMethod string
	HTTPPath   string
}

// Reporter persists / forwards Events.
type Reporter interface {
	Capture(ctx context.Context, e Event)
	Flush(ctx context.Context) error
}

// ── default global reporter ───────────────────────────────────────────────

var (
	mu  sync.RWMutex
	def Reporter = NewStderrReporter()
)

// SetDefault swaps the global reporter (call at boot from your
// orchestrator). nil disables capture.
func SetDefault(r Reporter) {
	mu.Lock()
	defer mu.Unlock()
	def = r
}

// Default returns the current reporter.
func Default() Reporter {
	mu.RLock()
	defer mu.RUnlock()
	return def
}

// Capture is the convenience wrapper most call sites use.
func Capture(ctx context.Context, e Event) {
	r := Default()
	if r == nil {
		return
	}
	if e.Severity == "" {
		e.Severity = SevError
	}
	r.Capture(ctx, e)
}

// CaptureErr is shorthand for "report this error".
func CaptureErr(ctx context.Context, err error, msg string) {
	if err == nil {
		return
	}
	Capture(ctx, Event{Severity: SevError, Message: msg, Err: err})
}

// ── built-in stderr reporter ──────────────────────────────────────────────

// StderrReporter prints every event as a single text line. Good
// default for dev and for any prod where you ship stderr to your log
// aggregator already.
type StderrReporter struct{ mu sync.Mutex }

func NewStderrReporter() *StderrReporter { return &StderrReporter{} }

func (s *StderrReporter) Capture(_ context.Context, e Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fmt.Fprintf(os.Stderr, "[%s] %s err=%v tags=%v\n",
		e.Severity, e.Message, e.Err, e.Tags)
	if e.Stack != "" {
		fmt.Fprintln(os.Stderr, e.Stack)
	}
}

func (s *StderrReporter) Flush(_ context.Context) error { return nil }

// ── HTTP middleware: turn panics into reports ─────────────────────────────

// RecoveryMiddleware catches panics in the wrapped handler, captures a
// SevFatal Event (with stack), writes a 500, and continues serving.
// Wrap as the *outermost* handler middleware so it sees panics from
// every other middleware too.
func RecoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				Capture(r.Context(), Event{
					Severity:   SevFatal,
					Message:    fmt.Sprintf("panic: %v", rec),
					Stack:      string(debug.Stack()),
					HTTPMethod: r.Method,
					HTTPPath:   r.URL.Path,
				})
				// Best-effort 500. If headers already went out, no-op.
				defer func() { _ = recover() }()
				http.Error(w, "internal server error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

// ── memory reporter for tests ─────────────────────────────────────────────

// MemoryReporter records events into a slice for test assertions.
type MemoryReporter struct {
	mu     sync.Mutex
	Events []Event
}

func NewMemoryReporter() *MemoryReporter { return &MemoryReporter{} }

func (m *MemoryReporter) Capture(_ context.Context, e Event) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Events = append(m.Events, e)
}

func (m *MemoryReporter) Flush(_ context.Context) error { return nil }

func (m *MemoryReporter) Snapshot() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.Events))
	copy(out, m.Events)
	return out
}
