package bundler

import (
	"context"
	"log"
	"os"
	"time"
)

type fileSnapshot struct {
	modTime time.Time
	size    int64
}

// StartWatcher polls source files for changes and rebuilds when they change.
// It returns immediately; the watch loop runs in a background goroutine.
// The loop exits when ctx is cancelled.
func StartWatcher(cfg Config, ctx context.Context) {
	debounce := time.Duration(cfg.WatchDebounceMs) * time.Millisecond
	if debounce <= 0 {
		debounce = 300 * time.Millisecond
	}

	go func() {
		snapshots := snapshotFiles(cfg)
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()

		var pending *time.Timer

		for {
			select {
			case <-ctx.Done():
				if pending != nil {
					pending.Stop()
				}
				return
			case <-ticker.C:
				current := snapshotFiles(cfg)
				if !snapshotsEqual(snapshots, current) {
					snapshots = current
					if pending != nil {
						pending.Stop()
					}
					pending = time.AfterFunc(debounce, func() {
						log.Println("[BUNDLE] File change detected, rebuilding...")
						if err := Run(cfg); err != nil {
							log.Printf("[BUNDLE] Rebuild failed: %v", err)
						}
					})
				}
			}
		}
	}()
}

// snapshotFiles collects mtime+size for every file the bundler cares about.
// It re-resolves globs on every call so new files are detected automatically.
func snapshotFiles(cfg Config) map[string]fileSnapshot {
	snap := make(map[string]fileSnapshot)

	record := func(path string) {
		if info, err := os.Stat(path); err == nil {
			snap[path] = fileSnapshot{modTime: info.ModTime(), size: info.Size()}
		}
	}

	// JS files
	if paths, err := resolveGlobs(cfg.JSFiles); err == nil {
		for _, p := range paths {
			record(p)
		}
	}

	// Template files
	for _, t := range cfg.Templates {
		if paths, err := resolveGlobs([]string{t.Pattern}); err == nil {
			for _, p := range paths {
				record(p)
			}
		}
	}

	// index template
	if cfg.IndexTemplate != "" {
		record(cfg.IndexTemplate)
	}

	// dependency sources
	for _, dep := range cfg.Dependencies {
		record(dep.Src)
	}

	return snap
}

func snapshotsEqual(a, b map[string]fileSnapshot) bool {
	if len(a) != len(b) {
		return false
	}
	for path, sa := range a {
		sb, ok := b[path]
		if !ok || sa.modTime != sb.modTime || sa.size != sb.size {
			return false
		}
	}
	return true
}
