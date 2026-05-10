// Package cron is a tiny scheduler for periodic in-process jobs. It
// supports two trigger forms:
//
//	every: 30s     — fixed interval (Go duration)
//	at:    "07:30" — daily at HH:MM (server local time)
//
// Designed for kicking plugin invocations, refresh tasks, periodic
// metric scrapes — not as a replacement for a real distributed
// scheduler. Single-process, in-memory, no persistence; missed ticks
// during downtime are NOT replayed.
package cron

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Job is one scheduled task.
type Job struct {
	Name string
	Run  func(ctx context.Context)

	Every    time.Duration // mutually exclusive with At
	At       string        // "HH:MM"

	runs    atomic.Int64
	failures atomic.Int64
}

// Scheduler ticks every second and runs eligible jobs.
type Scheduler struct {
	mu     sync.Mutex
	jobs   []*Job
	cancel context.CancelFunc
	wg     sync.WaitGroup
	now    func() time.Time

	last map[string]time.Time
}

// New constructs an empty scheduler.
func New() *Scheduler {
	return &Scheduler{now: time.Now, last: map[string]time.Time{}}
}

// Add registers a job. Validate-on-add so misconfig fails at boot.
func (s *Scheduler) Add(j *Job) error {
	if j.Name == "" {
		return fmt.Errorf("cron: empty job name")
	}
	if j.Run == nil {
		return fmt.Errorf("cron: job %q has nil Run func", j.Name)
	}
	if j.Every == 0 && j.At == "" {
		return fmt.Errorf("cron: job %q needs `every` or `at`", j.Name)
	}
	if j.At != "" {
		if _, _, err := parseHHMM(j.At); err != nil {
			return fmt.Errorf("cron: job %q bad at: %w", j.Name, err)
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.jobs = append(s.jobs, j)
	return nil
}

// Start the scheduler loop. Returns a cancel func; equivalent to Stop().
// Internal tick is 100ms so sub-second `every:` durations work.
func (s *Scheduler) Start(ctx context.Context) func() {
	ctx, s.cancel = context.WithCancel(ctx)
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		// Fire an immediate pass so jobs don't wait a full tick.
		s.tick(ctx, s.now())
		t := time.NewTicker(100 * time.Millisecond)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				s.tick(ctx, s.now())
			}
		}
	}()
	return s.Stop
}

// Stop cancels and waits for the loop.
func (s *Scheduler) Stop() {
	if s.cancel != nil {
		s.cancel()
	}
	s.wg.Wait()
}

func (s *Scheduler) tick(ctx context.Context, now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, j := range s.jobs {
		if s.due(j, now) {
			s.last[j.Name] = now
			go s.run(ctx, j)
		}
	}
}

func (s *Scheduler) due(j *Job, now time.Time) bool {
	last, ok := s.last[j.Name]
	switch {
	case j.Every > 0:
		if !ok {
			return true
		}
		return now.Sub(last) >= j.Every
	case j.At != "":
		hh, mm, _ := parseHHMM(j.At)
		if now.Hour() != hh || now.Minute() != mm {
			return false
		}
		// Avoid running twice in the same minute.
		if ok && now.Sub(last) < time.Minute {
			return false
		}
		return true
	}
	return false
}

func (s *Scheduler) run(ctx context.Context, j *Job) {
	j.runs.Add(1)
	defer func() {
		if r := recover(); r != nil {
			j.failures.Add(1)
		}
	}()
	j.Run(ctx)
}

// Snapshot returns the current job list with run counters for the
// admin dashboard.
type JobInfo struct {
	Name     string
	Every    time.Duration
	At       string
	Runs     int64
	Failures int64
	LastRun  time.Time
}

func (s *Scheduler) Snapshot() []JobInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]JobInfo, 0, len(s.jobs))
	for _, j := range s.jobs {
		out = append(out, JobInfo{
			Name: j.Name, Every: j.Every, At: j.At,
			Runs: j.runs.Load(), Failures: j.failures.Load(),
			LastRun: s.last[j.Name],
		})
	}
	sort.Slice(out, func(i, k int) bool { return out[i].Name < out[k].Name })
	return out
}

func parseHHMM(s string) (int, int, error) {
	parts := strings.SplitN(s, ":", 2)
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("expected HH:MM, got %q", s)
	}
	hh, err := strconv.Atoi(parts[0])
	if err != nil || hh < 0 || hh > 23 {
		return 0, 0, fmt.Errorf("bad hour")
	}
	mm, err := strconv.Atoi(parts[1])
	if err != nil || mm < 0 || mm > 59 {
		return 0, 0, fmt.Errorf("bad minute")
	}
	return hh, mm, nil
}

// ── default global scheduler ──────────────────────────────────────────────

var (
	mu       sync.RWMutex
	defaultS *Scheduler
)

func SetDefault(s *Scheduler) {
	mu.Lock()
	defer mu.Unlock()
	defaultS = s
}

func Default() *Scheduler {
	mu.RLock()
	defer mu.RUnlock()
	return defaultS
}
