// Package audit emits structured audit events for security-relevant
// actions (auth success/failure, plugin calls that returned non-2xx,
// admin views, etc.) to a configurable sink.
//
// Two sinks are built in:
//   - Stderr (default; line-delimited JSON)
//   - File   (line-delimited JSON, append mode)
//
// Custom sinks plug in by implementing Sink. SIEM-friendly downstream
// (Splunk, Elastic, Datadog) since every line is a self-contained JSON
// object with predictable keys.
package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
)

// Event is the canonical audit record. Keep keys snake_case so the JSON
// output is friendly to standard log pipelines.
type Event struct {
	Time      time.Time      `json:"time"`
	Action    string         `json:"action"`              // e.g. "auth.login", "plugin.call", "admin.view"
	Actor     string         `json:"actor,omitempty"`     // username or service id
	Target    string         `json:"target,omitempty"`    // resource path/name
	Outcome   string         `json:"outcome"`             // "success" | "failure" | "denied"
	IP        string         `json:"ip,omitempty"`
	UserAgent string         `json:"user_agent,omitempty"`
	RequestID string         `json:"request_id,omitempty"`
	Error     string         `json:"error,omitempty"`
	Meta      map[string]any `json:"meta,omitempty"`
}

// Sink is anything that can persist a single Event.
type Sink interface {
	Emit(Event) error
}

// ── default global sink ───────────────────────────────────────────────────

var (
	mu         sync.RWMutex
	defaultSnk Sink = NewWriterSink(os.Stderr)
)

// SetDefault swaps the global sink. nil disables auditing entirely.
func SetDefault(s Sink) {
	mu.Lock()
	defer mu.Unlock()
	defaultSnk = s
}

// Default returns the current sink (may be nil).
func Default() Sink {
	mu.RLock()
	defer mu.RUnlock()
	return defaultSnk
}

// Emit is the convenience wrapper most call sites use. Stamps Time if
// the caller didn't, swallows sink errors (audit must never break the
// hot path) but writes them to stderr so misconfig is visible.
func Emit(e Event) {
	s := Default()
	if s == nil {
		return
	}
	if e.Time.IsZero() {
		e.Time = time.Now().UTC()
	}
	if err := s.Emit(e); err != nil {
		fmt.Fprintf(os.Stderr, "audit emit failed: %v\n", err)
	}
}

// ── built-in sinks ────────────────────────────────────────────────────────

// WriterSink is the simplest concrete sink: one JSON object per line,
// flushed on every write. Safe for concurrent callers.
type WriterSink struct {
	mu sync.Mutex
	w  io.Writer
}

func NewWriterSink(w io.Writer) *WriterSink { return &WriterSink{w: w} }

func (s *WriterSink) Emit(e Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.w.Write(b); err != nil {
		return err
	}
	_, err = s.w.Write([]byte("\n"))
	return err
}

// FileSink opens (or creates) path in append mode. Caller may Close on
// shutdown; for long-lived processes this is optional.
type FileSink struct {
	*WriterSink
	f *os.File
}

func NewFileSink(path string) (*FileSink, error) {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &FileSink{WriterSink: NewWriterSink(f), f: f}, nil
}

func (s *FileSink) Close() error { return s.f.Close() }

// MultiSink fans every event out to N sinks. Errors from any sink are
// joined into one error.
type MultiSink struct {
	sinks []Sink
}

func NewMultiSink(sinks ...Sink) *MultiSink { return &MultiSink{sinks: sinks} }

func (m *MultiSink) Emit(e Event) error {
	var firstErr error
	for _, s := range m.sinks {
		if err := s.Emit(e); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
