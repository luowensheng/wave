// Package migrate is a tiny SQL migration runner for SQLite. Each
// migration is a numbered .sql file dropped in a directory:
//
//	migrations/
//	  0001_initial.up.sql
//	  0001_initial.down.sql
//	  0002_add_billing.up.sql
//	  0002_add_billing.down.sql
//
// State lives in a `_wave_migrations` table inside the same DB,
// so checkout-then-migrate is idempotent. We don't try to compete with
// `golang-migrate` — this is an honest 100-line implementation that
// covers the 90% case with zero new dependencies.
package migrate

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// Migration is one numbered file pair (up + optional down).
type Migration struct {
	Version int
	Name    string
	UpSQL   string
	DownSQL string // may be ""
}

// Discover scans dir for *.up.sql / *.down.sql files and returns one
// Migration per version, sorted ascending. Filenames must match
//
//	<NNNN>_<name>.up.sql
//	<NNNN>_<name>.down.sql
func Discover(dir string) ([]Migration, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	byVersion := map[int]*Migration{}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		v, name, kind, ok := parseFilename(e.Name())
		if !ok {
			continue
		}
		body, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		m := byVersion[v]
		if m == nil {
			m = &Migration{Version: v, Name: name}
			byVersion[v] = m
		}
		switch kind {
		case "up":
			m.UpSQL = string(body)
		case "down":
			m.DownSQL = string(body)
		}
	}
	out := make([]Migration, 0, len(byVersion))
	for _, m := range byVersion {
		out = append(out, *m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Version < out[j].Version })
	return out, nil
}

func parseFilename(name string) (version int, basename, kind string, ok bool) {
	if !strings.HasSuffix(name, ".sql") {
		return 0, "", "", false
	}
	stem := strings.TrimSuffix(name, ".sql")
	switch {
	case strings.HasSuffix(stem, ".up"):
		kind = "up"
		stem = strings.TrimSuffix(stem, ".up")
	case strings.HasSuffix(stem, ".down"):
		kind = "down"
		stem = strings.TrimSuffix(stem, ".down")
	default:
		return 0, "", "", false
	}
	idx := strings.IndexByte(stem, '_')
	if idx <= 0 {
		return 0, "", "", false
	}
	v, err := strconv.Atoi(stem[:idx])
	if err != nil {
		return 0, "", "", false
	}
	return v, stem[idx+1:], kind, true
}

// State tracks applied versions.
type State struct {
	db *sql.DB
}

func NewState(db *sql.DB) (*State, error) {
	s := &State{db: db}
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS _wave_migrations (
		version INTEGER PRIMARY KEY,
		name TEXT NOT NULL,
		applied_at TEXT NOT NULL DEFAULT (datetime('now'))
	)`); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *State) Applied() (map[int]bool, error) {
	rows, err := s.db.Query("SELECT version FROM _wave_migrations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int]bool{}
	for rows.Next() {
		var v int
		if err := rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, nil
}

// Up applies every pending migration in version order. Each migration
// runs inside its own transaction; a failed migration leaves the DB at
// the previous version.
func Up(db *sql.DB, dir string) ([]Migration, error) {
	migs, err := Discover(dir)
	if err != nil {
		return nil, err
	}
	state, err := NewState(db)
	if err != nil {
		return nil, err
	}
	applied, err := state.Applied()
	if err != nil {
		return nil, err
	}
	var ran []Migration
	for _, m := range migs {
		if applied[m.Version] {
			continue
		}
		if err := runOne(db, m, true); err != nil {
			return ran, fmt.Errorf("migration %04d_%s up: %w", m.Version, m.Name, err)
		}
		ran = append(ran, m)
	}
	return ran, nil
}

// Down rolls back the last applied migration (one step). Returns the
// migration that was rolled back, or nil if there's nothing to undo.
func Down(db *sql.DB, dir string) (*Migration, error) {
	migs, err := Discover(dir)
	if err != nil {
		return nil, err
	}
	state, err := NewState(db)
	if err != nil {
		return nil, err
	}
	applied, err := state.Applied()
	if err != nil {
		return nil, err
	}
	for i := len(migs) - 1; i >= 0; i-- {
		m := migs[i]
		if !applied[m.Version] {
			continue
		}
		if m.DownSQL == "" {
			return nil, fmt.Errorf("migration %04d_%s has no down SQL", m.Version, m.Name)
		}
		if err := runOne(db, m, false); err != nil {
			return nil, err
		}
		return &m, nil
	}
	return nil, nil
}

func runOne(db *sql.DB, m Migration, up bool) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	body := m.UpSQL
	if !up {
		body = m.DownSQL
	}
	if _, err := tx.Exec(body); err != nil {
		return err
	}
	if up {
		if _, err := tx.Exec("INSERT INTO _wave_migrations(version, name) VALUES (?, ?)",
			m.Version, m.Name); err != nil {
			return err
		}
	} else {
		if _, err := tx.Exec("DELETE FROM _wave_migrations WHERE version = ?", m.Version); err != nil {
			return err
		}
	}
	return tx.Commit()
}
