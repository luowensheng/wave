package studio

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// makeFakeBinary writes a small shell script that ignores its args,
// prints `script` to stdout/stderr, then exits with `exitCode`.
// Returns the absolute path to the executable script.
func makeFakeBinary(t *testing.T, script string, exitCode int) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake binary helper is shell-script based; skipping on windows")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "fake-wave.sh")
	body := "#!/bin/sh\n" + script + "\nexit " + itoa(exitCode) + "\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	digits := []byte{}
	for i > 0 {
		digits = append([]byte{byte('0' + i%10)}, digits...)
		i /= 10
	}
	if neg {
		return "-" + string(digits)
	}
	return string(digits)
}

func newProject(t *testing.T) *Project {
	t.Helper()
	d := t.TempDir()
	if err := os.WriteFile(filepath.Join(d, "server.yaml"), []byte("default:\n  port: 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &Project{ID: "p1", Name: "p1", Path: d, ConfigFile: "server.yaml"}
}

func TestSupervisorStartStop(t *testing.T) {
	bin := makeFakeBinary(t, `echo "hello-stdout"; >&2 echo "hello-stderr"; sleep 30`, 0)
	sup := NewSupervisor(bin)
	p := newProject(t)
	if err := sup.Start(p); err != nil {
		t.Fatalf("start: %v", err)
	}
	// Wait briefly for the process to actually run.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if proc, ok := sup.Status(p.ID); ok {
			st, pid, _ := proc.Snapshot()
			if st == StatusRunning && pid > 0 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	proc, ok := sup.Status(p.ID)
	if !ok {
		t.Fatalf("expected proc to exist")
	}
	st, pid, _ := proc.Snapshot()
	if st != StatusRunning {
		t.Fatalf("expected running, got %s", st)
	}
	if pid <= 0 {
		t.Fatalf("expected PID > 0, got %d", pid)
	}

	if err := sup.Stop(p.ID); err != nil {
		t.Fatalf("stop: %v", err)
	}
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if proc, _ := sup.Status(p.ID); proc != nil {
			st, _, _ := proc.Snapshot()
			if st == StatusStopped {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	proc, _ = sup.Status(p.ID)
	st, _, _ = proc.Snapshot()
	if st != StatusStopped {
		t.Errorf("expected stopped, got %s", st)
	}
}

func TestSupervisorLogCapture(t *testing.T) {
	bin := makeFakeBinary(t, `echo "line-one"; echo "line-two"; sleep 30`, 0)
	sup := NewSupervisor(bin)
	p := newProject(t)
	if err := sup.Start(p); err != nil {
		t.Fatal(err)
	}
	defer sup.Stop(p.ID)

	// Wait for log lines to land in ringbuffer.
	deadline := time.Now().Add(8 * time.Second)
	var snap []string
	for time.Now().Before(deadline) {
		proc, _ := sup.Status(p.ID)
		if proc != nil {
			snap = proc.logs.snapshot()
			if len(snap) >= 2 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	got := strings.Join(snap, "\n")
	if !strings.Contains(got, "line-one") || !strings.Contains(got, "line-two") {
		t.Errorf("ringbuffer missing lines: %q", got)
	}
}

func TestSupervisorSubscribeReceivesSnapshotAndLive(t *testing.T) {
	bin := makeFakeBinary(t, `echo "first"; sleep 0.2; echo "second"; sleep 30`, 0)
	sup := NewSupervisor(bin)
	p := newProject(t)
	if err := sup.Start(p); err != nil {
		t.Fatal(err)
	}
	defer sup.Stop(p.ID)

	// Wait for "first" to be in the buffer before subscribing.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		proc, _ := sup.Status(p.ID)
		if proc != nil && len(proc.logs.snapshot()) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	ch, cancel, err := sup.Subscribe(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer cancel()

	collected := []string{}
	timeout := time.After(8 * time.Second)
loop:
	for {
		select {
		case line, ok := <-ch:
			if !ok {
				break loop
			}
			collected = append(collected, line)
			if hasAll(collected, "first", "second") {
				break loop
			}
		case <-timeout:
			break loop
		}
	}
	if !hasAll(collected, "first", "second") {
		t.Errorf("expected snapshot+live: %v", collected)
	}
}

func hasAll(haystack []string, needles ...string) bool {
	for _, n := range needles {
		found := false
		for _, h := range haystack {
			if strings.Contains(h, n) {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func TestSupervisorRestartOnCrash(t *testing.T) {
	// Process exits non-zero immediately — supervisor should auto-restart.
	bin := makeFakeBinary(t, `echo "boom"`, 1)
	sup := NewSupervisor(bin)
	p := newProject(t)
	if err := sup.Start(p); err != nil {
		t.Fatal(err)
	}
	// Wait long enough for it to crash + at least one auto-restart cycle.
	time.Sleep(2 * time.Second)
	proc, ok := sup.Status(p.ID)
	if !ok {
		t.Fatal("missing proc")
	}
	_, _, restarts := proc.Snapshot()
	if restarts < 1 {
		t.Errorf("expected restarts >= 1, got %d", restarts)
	}
	// Cap is 3 restarts; eventually status should settle to crashed.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		st, _, r := proc.Snapshot()
		if st == StatusCrashed && r >= 3 {
			restarts = r
			break
		}
		restarts = r
		time.Sleep(100 * time.Millisecond)
	}
	if restarts > 3 {
		t.Errorf("restart cap exceeded: %d", restarts)
	}
}

func TestSupervisorRestartOnDemand(t *testing.T) {
	bin := makeFakeBinary(t, `sleep 30`, 0)
	sup := NewSupervisor(bin)
	p := newProject(t)
	if err := sup.Start(p); err != nil {
		t.Fatal(err)
	}
	defer sup.Stop(p.ID)

	// Wait until running.
	waitFor(t, func() bool {
		proc, ok := sup.Status(p.ID)
		if !ok {
			return false
		}
		st, _, _ := proc.Snapshot()
		return st == StatusRunning
	}, 2*time.Second)

	first, _ := sup.Status(p.ID)
	_, firstPID, _ := first.Snapshot()
	if err := sup.Restart(p.ID, p); err != nil {
		t.Fatalf("restart: %v", err)
	}
	waitFor(t, func() bool {
		proc, ok := sup.Status(p.ID)
		if !ok {
			return false
		}
		st, pid, _ := proc.Snapshot()
		return st == StatusRunning && pid != firstPID
	}, 3*time.Second)
	second, _ := sup.Status(p.ID)
	_, secondPID, _ := second.Snapshot()
	if secondPID == firstPID {
		t.Errorf("PID did not change on restart")
	}
}

func waitFor(t *testing.T, cond func() bool, d time.Duration) {
	t.Helper()
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Errorf("condition not met within %s", d)
}

// Sanity: ensure /bin/sh exists where these tests run.
func TestShAvailable(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
}
