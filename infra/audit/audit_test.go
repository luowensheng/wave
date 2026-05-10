package audit

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestWriterSinkLineDelimited(t *testing.T) {
	var buf bytes.Buffer
	s := NewWriterSink(&buf)
	if err := s.Emit(Event{Action: "auth.login", Outcome: "success", Actor: "alice"}); err != nil {
		t.Fatal(err)
	}
	if err := s.Emit(Event{Action: "auth.logout", Outcome: "success", Actor: "alice"}); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d", len(lines))
	}
	for i, l := range lines {
		var e Event
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Errorf("line %d not JSON: %v", i, err)
		}
		if e.Time.IsZero() {
			// SetDefault.Emit wrapper stamps Time, but the raw sink doesn't.
			// We allow zero here.
		}
	}
}

func TestEmitStampsTimeAndSwallowsErrors(t *testing.T) {
	var got Event
	SetDefault(funcSink(func(e Event) error { got = e; return errors.New("ignored") }))
	defer SetDefault(NewWriterSink(&bytes.Buffer{}))
	Emit(Event{Action: "x", Outcome: "denied"})
	if got.Time.IsZero() {
		t.Error("Time should be stamped")
	}
}

func TestMultiSinkFanout(t *testing.T) {
	var a, b bytes.Buffer
	m := NewMultiSink(NewWriterSink(&a), NewWriterSink(&b))
	if err := m.Emit(Event{Action: "x", Outcome: "success"}); err != nil {
		t.Fatal(err)
	}
	if a.Len() == 0 || b.Len() == 0 {
		t.Errorf("fanout failed: a=%d b=%d", a.Len(), b.Len())
	}
}

func TestConcurrentEmit(t *testing.T) {
	var buf bytes.Buffer
	s := NewWriterSink(&buf)
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Emit(Event{Action: "x", Outcome: "success"})
		}()
	}
	wg.Wait()
	lines := strings.Count(buf.String(), "\n")
	if lines != 100 {
		t.Errorf("want 100 lines, got %d", lines)
	}
}

type funcSink func(Event) error

func (f funcSink) Emit(e Event) error { return f(e) }
