package errreport

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCaptureUsesDefault(t *testing.T) {
	mem := NewMemoryReporter()
	SetDefault(mem)
	defer SetDefault(NewStderrReporter())

	Capture(context.Background(), Event{Message: "hi"})
	if len(mem.Snapshot()) != 1 {
		t.Errorf("got %d events", len(mem.Snapshot()))
	}
	if mem.Snapshot()[0].Severity != SevError {
		t.Errorf("default severity = %q", mem.Snapshot()[0].Severity)
	}
}

func TestCaptureErrSkipsNil(t *testing.T) {
	mem := NewMemoryReporter()
	SetDefault(mem)
	defer SetDefault(NewStderrReporter())

	CaptureErr(context.Background(), nil, "should be ignored")
	if len(mem.Snapshot()) != 0 {
		t.Errorf("nil error captured: %v", mem.Snapshot())
	}
	CaptureErr(context.Background(), errors.New("boom"), "real")
	if len(mem.Snapshot()) != 1 {
		t.Errorf("real error not captured: %d", len(mem.Snapshot()))
	}
}

func TestRecoveryMiddlewareCatchesPanic(t *testing.T) {
	mem := NewMemoryReporter()
	SetDefault(mem)
	defer SetDefault(NewStderrReporter())

	h := RecoveryMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("oh no")
	}))
	r := httptest.NewRequest("GET", "/x", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d", w.Code)
	}
	events := mem.Snapshot()
	if len(events) != 1 || events[0].Severity != SevFatal {
		t.Fatalf("events = %+v", events)
	}
	if !strings.Contains(events[0].Stack, "errreport_test.go") {
		t.Errorf("stack should mention test file: %q", events[0].Stack)
	}
}

func TestRecoveryMiddlewarePassesNormalRequest(t *testing.T) {
	h := RecoveryMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(202)
		_, _ = w.Write([]byte("ok"))
	}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 202 || w.Body.String() != "ok" {
		t.Errorf("status=%d body=%q", w.Code, w.Body.String())
	}
}
