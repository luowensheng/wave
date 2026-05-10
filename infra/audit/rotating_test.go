package audit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRotatingFileSinkRollsOver(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	// Tiny size to force rotation almost immediately.
	s, err := NewRotatingFileSink(path, 200, 2)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 30; i++ {
		if err := s.Emit(Event{Action: "x", Outcome: "success", Actor: "user-a-with-some-padding"}); err != nil {
			t.Fatal(err)
		}
	}
	segs, err := s.Segments()
	if err != nil {
		t.Fatal(err)
	}
	if len(segs) < 2 {
		t.Fatalf("expected at least 2 segments, got %v", segs)
	}
	// Oldest beyond keep=N should be gone (segment .3 must not exist).
	if _, err := os.Stat(path + ".3"); err == nil {
		t.Errorf("segment .3 should have been pruned")
	}
}

func TestRotatingFileSinkKeepZeroNoRotation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")
	s, err := NewRotatingFileSink(path, 50, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for i := 0; i < 20; i++ {
		_ = s.Emit(Event{Action: "x", Outcome: "success"})
	}
	if _, err := os.Stat(path + ".1"); err == nil {
		t.Errorf("keep=0 should not produce rotated segments")
	}
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), `"action":"x"`) {
		t.Errorf("file body missing entries: %q", body)
	}
}

func TestRotatingFileSinkSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.log")

	s1, _ := NewRotatingFileSink(path, 1024, 2)
	_ = s1.Emit(Event{Action: "first", Outcome: "success"})
	_ = s1.Close()

	s2, _ := NewRotatingFileSink(path, 1024, 2)
	defer s2.Close()
	_ = s2.Emit(Event{Action: "second", Outcome: "success"})
	body, _ := os.ReadFile(path)
	if !strings.Contains(string(body), "first") || !strings.Contains(string(body), "second") {
		t.Errorf("reopen lost or duplicated data: %q", body)
	}
}
