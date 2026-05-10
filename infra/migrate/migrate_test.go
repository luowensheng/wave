package migrate

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func writeMigration(t *testing.T, dir, name, up, down string) {
	t.Helper()
	if up != "" {
		if err := os.WriteFile(filepath.Join(dir, name+".up.sql"), []byte(up), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if down != "" {
		if err := os.WriteFile(filepath.Join(dir, name+".down.sql"), []byte(down), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestParseFilename(t *testing.T) {
	cases := []struct {
		in     string
		v      int
		name   string
		kind   string
		ok     bool
	}{
		{"0001_initial.up.sql", 1, "initial", "up", true},
		{"0042_add_billing.down.sql", 42, "add_billing", "down", true},
		{"junk.txt", 0, "", "", false},
		{"missing_kind.sql", 0, "", "", false},
		{"NaN_x.up.sql", 0, "", "", false},
	}
	for _, c := range cases {
		v, n, k, ok := parseFilename(c.in)
		if ok != c.ok || v != c.v || n != c.name || k != c.kind {
			t.Errorf("parseFilename(%q) = (%d,%q,%q,%v), want (%d,%q,%q,%v)",
				c.in, v, n, k, ok, c.v, c.name, c.kind, c.ok)
		}
	}
}

func TestUpAppliesPendingMigrations(t *testing.T) {
	dir := t.TempDir()
	writeMigration(t, dir, "0001_init",
		`CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)`,
		`DROP TABLE items`)
	writeMigration(t, dir, "0002_add_email",
		`ALTER TABLE items ADD COLUMN email TEXT`,
		`-- can't drop columns in sqlite, leave intentionally empty test stub`)

	db := openDB(t)
	ran, err := Up(db, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ran) != 2 {
		t.Errorf("ran = %d migrations", len(ran))
	}

	// Idempotent: running Up again applies nothing.
	ran2, err := Up(db, dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(ran2) != 0 {
		t.Errorf("re-Up ran %d, want 0", len(ran2))
	}

	// Verify the schema is real.
	if _, err := db.Exec(`INSERT INTO items(name, email) VALUES ('a', 'b@c')`); err != nil {
		t.Errorf("insert post-migrate: %v", err)
	}
}

func TestDownReversesLast(t *testing.T) {
	dir := t.TempDir()
	writeMigration(t, dir, "0001_init",
		`CREATE TABLE x (id INTEGER PRIMARY KEY)`,
		`DROP TABLE x`)
	db := openDB(t)
	if _, err := Up(db, dir); err != nil {
		t.Fatal(err)
	}
	got, err := Down(db, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Version != 1 {
		t.Fatalf("Down returned %v", got)
	}
	if _, err := db.Exec(`INSERT INTO x DEFAULT VALUES`); err == nil {
		t.Error("table x should be gone after Down")
	}
}

func TestDownNoOpWhenNothingApplied(t *testing.T) {
	dir := t.TempDir()
	writeMigration(t, dir, "0001_init", `SELECT 1`, `SELECT 1`)
	db := openDB(t)
	got, err := Down(db, dir)
	if err != nil || got != nil {
		t.Errorf("Down on empty state: got=%v err=%v", got, err)
	}
}

func TestUpFailureLeavesPreviousVersion(t *testing.T) {
	dir := t.TempDir()
	writeMigration(t, dir, "0001_ok",
		`CREATE TABLE a (id INTEGER PRIMARY KEY)`, `DROP TABLE a`)
	writeMigration(t, dir, "0002_broken",
		`THIS IS NOT VALID SQL`, ``)
	db := openDB(t)
	ran, err := Up(db, dir)
	if err == nil {
		t.Error("expected error on broken migration")
	}
	if len(ran) != 1 || ran[0].Version != 1 {
		t.Errorf("expected only v1 to apply, got %v", ran)
	}
	st, _ := NewState(db)
	applied, _ := st.Applied()
	if applied[2] {
		t.Error("v2 should not be marked applied")
	}
	if !applied[1] {
		t.Error("v1 should remain applied")
	}
}
