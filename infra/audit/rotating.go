package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
)

// RotatingFileSink writes line-delimited JSON to a path and rolls the
// file over once it exceeds MaxBytes. Old segments are renamed
// `<path>.1`, `<path>.2`, ... up to KeepN; the oldest is deleted on
// each rotation. Designed for low-volume audit traffic — for high-rate
// access logging use an external log shipper instead.
type RotatingFileSink struct {
	path     string
	maxBytes int64
	keep     int

	mu   sync.Mutex
	file *os.File
	size int64
}

// NewRotatingFileSink opens (or creates) `path` in append mode, capped
// at maxBytes per file with `keep` historical segments retained.
// keep=0 disables rotation (the sink behaves like a basic FileSink).
func NewRotatingFileSink(path string, maxBytes int64, keep int) (*RotatingFileSink, error) {
	if maxBytes <= 0 {
		maxBytes = 10 << 20 // 10 MiB
	}
	if keep < 0 {
		keep = 0
	}
	s := &RotatingFileSink{path: path, maxBytes: maxBytes, keep: keep}
	if err := s.open(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *RotatingFileSink) open() error {
	f, err := os.OpenFile(s.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return err
	}
	s.file = f
	s.size = info.Size()
	return nil
}

func (s *RotatingFileSink) Emit(e Event) error {
	b, err := json.Marshal(e)
	if err != nil {
		return err
	}
	line := append(b, '\n')

	s.mu.Lock()
	defer s.mu.Unlock()

	// Rotate before write so the new line lands in the fresh segment.
	if s.keep > 0 && s.size+int64(len(line)) > s.maxBytes {
		if err := s.rotateLocked(); err != nil {
			return err
		}
	}
	n, err := s.file.Write(line)
	s.size += int64(n)
	return err
}

func (s *RotatingFileSink) rotateLocked() error {
	if err := s.file.Close(); err != nil {
		return fmt.Errorf("close: %w", err)
	}
	// Shift .1 → .2, .2 → .3, ..., dropping the oldest.
	for i := s.keep; i >= 1; i-- {
		from := s.numbered(i)
		if i == s.keep {
			_ = os.Remove(from) // best-effort drop oldest
			continue
		}
		to := s.numbered(i + 1)
		if _, err := os.Stat(from); err == nil {
			if err := os.Rename(from, to); err != nil {
				return fmt.Errorf("rotate %s→%s: %w", from, to, err)
			}
		}
	}
	if err := os.Rename(s.path, s.numbered(1)); err != nil {
		return fmt.Errorf("rotate current→.1: %w", err)
	}
	return s.open()
}

func (s *RotatingFileSink) numbered(i int) string {
	return s.path + "." + strconv.Itoa(i)
}

// Close flushes and closes the underlying file. Optional but tidy.
func (s *RotatingFileSink) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.file == nil {
		return nil
	}
	err := s.file.Close()
	s.file = nil
	return err
}

// Segments lists the current and historical files for inspection /
// tests. Returns paths sorted from newest (current) to oldest.
func (s *RotatingFileSink) Segments() ([]string, error) {
	dir := filepath.Dir(s.path)
	base := filepath.Base(s.path)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var matches []string
	for _, e := range entries {
		n := e.Name()
		if n == base || strings.HasPrefix(n, base+".") {
			matches = append(matches, filepath.Join(dir, n))
		}
	}
	// Current first, then .1, .2, …
	sort.Slice(matches, func(i, j int) bool {
		ai := suffixIndex(matches[i], base)
		aj := suffixIndex(matches[j], base)
		return ai < aj
	})
	return matches, nil
}

func suffixIndex(path, base string) int {
	n := filepath.Base(path)
	if n == base {
		return 0
	}
	rest := strings.TrimPrefix(n, base+".")
	v, err := strconv.Atoi(rest)
	if err != nil {
		return 1<<30 // sort unknown to the end
	}
	return v
}
