package cron

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

func TestEveryFires(t *testing.T) {
	s := New()
	var n atomic.Int32
	if err := s.Add(&Job{Name: "tick", Every: 200 * time.Millisecond, Run: func(_ context.Context) {
		n.Add(1)
	}}); err != nil {
		t.Fatal(err)
	}
	stop := s.Start(context.Background())
	defer stop()
	time.Sleep(700 * time.Millisecond) // expect ~3 fires (immediate + 2 ticks)
	if got := n.Load(); got < 2 {
		t.Errorf("got %d fires, want >=2", got)
	}
}

func TestAtFiresInExactMinute(t *testing.T) {
	s := New()
	var n atomic.Int32
	now := time.Date(2026, 5, 3, 7, 30, 0, 0, time.Local)
	s.now = func() time.Time { return now }
	_ = s.Add(&Job{Name: "morning", At: "07:30", Run: func(_ context.Context) {
		n.Add(1)
	}})
	// Manually tick — bypass the goroutine loop for determinism.
	s.tick(context.Background(), now)
	time.Sleep(20 * time.Millisecond)
	if n.Load() != 1 {
		t.Errorf("got %d", n.Load())
	}
	// Same-minute re-tick must not double-fire.
	s.tick(context.Background(), now.Add(10*time.Second))
	time.Sleep(20 * time.Millisecond)
	if n.Load() != 1 {
		t.Errorf("re-tick fired again: %d", n.Load())
	}
}

func TestSnapshotCounters(t *testing.T) {
	s := New()
	_ = s.Add(&Job{Name: "a", Every: time.Second, Run: func(_ context.Context) {}})
	infos := s.Snapshot()
	if len(infos) != 1 || infos[0].Name != "a" {
		t.Fatalf("got %+v", infos)
	}
}

func TestAddValidatesConfig(t *testing.T) {
	s := New()
	if err := s.Add(&Job{Name: "x", Run: func(_ context.Context) {}}); err == nil {
		t.Error("missing every+at should fail")
	}
	if err := s.Add(&Job{Name: "x", At: "25:99", Run: func(_ context.Context) {}}); err == nil {
		t.Error("bad at should fail")
	}
	if err := s.Add(&Job{Name: "", Every: time.Second, Run: func(_ context.Context) {}}); err == nil {
		t.Error("empty name should fail")
	}
}

func TestRecoverFromPanic(t *testing.T) {
	s := New()
	_ = s.Add(&Job{Name: "boom", Every: 50 * time.Millisecond, Run: func(_ context.Context) {
		panic("oops")
	}})
	stop := s.Start(context.Background())
	defer stop()
	time.Sleep(150 * time.Millisecond)
	infos := s.Snapshot()
	if infos[0].Failures < 1 {
		t.Errorf("expected failures > 0: %+v", infos[0])
	}
}
