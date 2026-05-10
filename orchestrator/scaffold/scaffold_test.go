package scaffold

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAllTemplatesAreNonEmpty(t *testing.T) {
	for _, tpl := range All() {
		if tpl.Name == "" || tpl.Description == "" || len(tpl.Files) == 0 {
			t.Errorf("template %q is missing fields: %+v", tpl.Name, tpl)
		}
		if _, ok := tpl.Files["server.yaml"]; !ok {
			t.Errorf("template %q missing server.yaml", tpl.Name)
		}
	}
}

func TestRenderWritesFiles(t *testing.T) {
	dir := t.TempDir()
	tpl, _ := Get("api")
	if err := Render(tpl, dir, false); err != nil {
		t.Fatal(err)
	}
	for rel := range tpl.Files {
		p := filepath.Join(dir, rel)
		if _, err := os.Stat(p); err != nil {
			t.Errorf("file missing: %s: %v", rel, err)
		}
	}
}

func TestRenderRefusesNonEmpty(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "x"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	tpl, _ := Get("api")
	if err := Render(tpl, dir, false); err == nil {
		t.Error("expected error on non-empty dir without --force")
	}
	if err := Render(tpl, dir, true); err != nil {
		t.Errorf("--force should overwrite: %v", err)
	}
}

func TestGetUnknown(t *testing.T) {
	if _, ok := Get("does-not-exist"); ok {
		t.Error("Get returned true for unknown template")
	}
}
