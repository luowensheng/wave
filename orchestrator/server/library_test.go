package servers

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile is a tiny helper for building fixture trees in a temp dir.
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLibrary_KindParse(t *testing.T) {
	dir := t.TempDir()
	cache := newLibraryCache()

	// missing kind
	p := writeFile(t, dir, "nokind.yaml", "db: { type: sqlite, path: ./x.db }\n")
	if _, err := cache.loadTypedLibrary(p); err == nil || !strings.Contains(err.Error(), "missing top-level `kind:`") {
		t.Fatalf("missing kind: got %v", err)
	}

	// invalid kind
	p = writeFile(t, dir, "badkind.yaml", "kind: widgets\nfoo: {}\n")
	if _, err := cache.loadTypedLibrary(p); err == nil || !strings.Contains(err.Error(), "invalid kind") {
		t.Fatalf("invalid kind: got %v", err)
	}

	// extra server-only block (routes:)
	p = writeFile(t, dir, "withroutes.yaml", "kind: storage\ndb: { type: sqlite, path: ./x.db }\nroutes:\n  - path: /a\n")
	if _, err := cache.loadTypedLibrary(p); err == nil || !strings.Contains(err.Error(), "must not declare `routes:`") {
		t.Fatalf("extra routes block: got %v", err)
	}

	// foreign resource kind inside a library
	p = writeFile(t, dir, "mixed.yaml", "kind: storage\ndb: { type: sqlite, path: ./x.db }\nplugins:\n  p: { transport: http, address: 'http://x' }\n")
	if _, err := cache.loadTypedLibrary(p); err == nil || !strings.Contains(err.Error(), "must not also declare") {
		t.Fatalf("foreign kind: got %v", err)
	}

	// valid storage lib (root form)
	p = writeFile(t, dir, "ok.yaml", "kind: storage\ndb: { type: sqlite, path: ./x.db }\n")
	lib, err := cache.loadTypedLibrary(p)
	if err != nil {
		t.Fatalf("valid lib: %v", err)
	}
	if lib.kind != "storage" || len(lib.nodes) != 1 {
		t.Fatalf("lib = %+v", lib)
	}

	// missing file
	if _, err := cache.loadTypedLibrary(filepath.Join(dir, "nope.yaml")); err == nil || !strings.Contains(err.Error(), "cannot read library file") {
		t.Fatalf("missing file: got %v", err)
	}
}

func TestLibrary_CacheIdentity(t *testing.T) {
	dir := t.TempDir()
	p := writeFile(t, dir, "db.yaml", "kind: storage\ndb: { type: sqlite, path: ./x.db }\n")
	cache := newLibraryCache()
	a, err := cache.loadTypedLibrary(p)
	if err != nil {
		t.Fatal(err)
	}
	b, err := cache.loadTypedLibrary(p)
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Fatal("expected cached library to be the same pointer")
	}
}

func TestLibrary_JSONFormat(t *testing.T) {
	dir := t.TempDir()
	// JSON is a YAML 1.2 subset — same loader, no extension sniffing.
	p := writeFile(t, dir, "db.json", `{"kind":"storage","db":{"type":"sqlite","path":"./x.db"}}`)
	cache := newLibraryCache()
	lib, err := cache.loadTypedLibrary(p)
	if err != nil {
		t.Fatalf("json lib: %v", err)
	}
	if lib.kind != "storage" {
		t.Fatalf("kind = %q", lib.kind)
	}
	node := lib.nodes["db"]
	v, err := decodeResource("storage", &node)
	if err != nil {
		t.Fatal(err)
	}
	if v == nil {
		t.Fatal("nil storage value")
	}
}
