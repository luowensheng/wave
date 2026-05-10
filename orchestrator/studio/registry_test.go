package studio

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func writeYAML(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRegistryRoundTrip(t *testing.T) {
	dataDir := t.TempDir()
	projDir := t.TempDir()
	writeYAML(t, projDir, "server.yaml", "name: my-test\ndefault:\n  host: localhost\n  port: 8080\n")

	r, err := LoadRegistry(dataDir)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got := len(r.List()); got != 0 {
		t.Fatalf("want empty, got %d", got)
	}
	p, err := r.Add(projDir, "server.yaml")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if p.Name != "my-test" {
		t.Errorf("name from yaml: got %q want my-test", p.Name)
	}

	// Re-read fresh registry
	r2, err := LoadRegistry(dataDir)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if got := len(r2.List()); got != 1 {
		t.Fatalf("after reload want 1, got %d", got)
	}
	if g, ok := r2.Get(p.ID); !ok || g.Name != "my-test" {
		t.Errorf("get: %+v ok=%v", g, ok)
	}

	// Duplicate add must fail
	if _, err := r2.Add(projDir, "server.yaml"); err == nil {
		t.Error("duplicate add should fail")
	}

	// Remove
	if err := r2.Remove(p.ID); err != nil {
		t.Fatalf("remove: %v", err)
	}
	if got := len(r2.List()); got != 0 {
		t.Fatalf("after remove want 0, got %d", got)
	}
}

func TestRegistryAddMissingConfigFails(t *testing.T) {
	dataDir := t.TempDir()
	projDir := t.TempDir()
	r, _ := LoadRegistry(dataDir)
	if _, err := r.Add(projDir, "server.yaml"); err == nil {
		t.Error("expected error when config file is missing")
	}
}

func TestRegistryAddNonDirFails(t *testing.T) {
	dataDir := t.TempDir()
	r, _ := LoadRegistry(dataDir)
	if _, err := r.Add("/this/path/does/not/exist", "server.yaml"); err == nil {
		t.Error("expected error when path missing")
	}
}

func TestRegistryConcurrentAdd(t *testing.T) {
	dataDir := t.TempDir()
	r, _ := LoadRegistry(dataDir)

	const N = 10
	var wg sync.WaitGroup
	for i := 0; i < N; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			d := t.TempDir()
			writeYAML(t, d, "server.yaml", "name: x\n")
			_, _ = r.Add(d, "server.yaml")
		}(i)
	}
	wg.Wait()

	// Re-load — file should be valid JSON regardless.
	r2, err := LoadRegistry(dataDir)
	if err != nil {
		t.Fatalf("reload after concurrent writes: %v", err)
	}
	if got := len(r2.List()); got != N {
		t.Errorf("want %d projects, got %d", N, got)
	}
}

func TestStableID(t *testing.T) {
	a := stableID("/a", "server.yaml")
	b := stableID("/a", "server.yaml")
	c := stableID("/b", "server.yaml")
	if a != b {
		t.Error("same input must give same id")
	}
	if a == c {
		t.Error("different paths must give different ids")
	}
}
