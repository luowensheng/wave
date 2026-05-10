package studio

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"syscall"
	"time"
)

// Status values for a supervised process.
const (
	StatusStopped  = "stopped"
	StatusStarting = "starting"
	StatusRunning  = "running"
	StatusCrashed  = "crashed"
	StatusStopping = "stopping"
)

// Proc tracks one supervised wave child process.
type Proc struct {
	ProjectID string    `json:"project_id"`
	Status    string    `json:"status"`
	PID       int       `json:"pid"`
	StartedAt time.Time `json:"started_at"`
	Restarts  int       `json:"restarts"`

	cmd         *exec.Cmd
	logs        *ringBuffer
	subscribers map[chan string]struct{}
	subMu       sync.Mutex
	mu          sync.Mutex

	// crash auto-restart bookkeeping
	restartWindow []time.Time
	stopCh        chan struct{} // closed on Stop to suppress auto-restart
}

// Uptime returns how long the process has been in StatusRunning.
func (p *Proc) Uptime() time.Duration {
	if p.Status != StatusRunning {
		return 0
	}
	return time.Since(p.StartedAt)
}

// Snapshot returns a goroutine-safe copy of the volatile proc fields.
func (p *Proc) Snapshot() (status string, pid int, restarts int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.Status, p.PID, p.Restarts
}

// Supervisor manages all supervised child processes.
type Supervisor struct {
	binaryPath string
	procs      map[string]*Proc
	mu         sync.RWMutex
}

// NewSupervisor returns a supervisor that will spawn binaryPath when
// asked to start a project.
func NewSupervisor(binaryPath string) *Supervisor {
	return &Supervisor{
		binaryPath: binaryPath,
		procs:      map[string]*Proc{},
	}
}

// BinaryPath returns the executable used to start child processes.
func (s *Supervisor) BinaryPath() string { return s.binaryPath }

// Status returns a snapshot of the proc state for projectID, or false.
func (s *Supervisor) Status(projectID string) (*Proc, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	p, ok := s.procs[projectID]
	return p, ok
}

// Start launches the wave child process for project p.
func (s *Supervisor) Start(p *Project) error {
	s.mu.Lock()
	existing, ok := s.procs[p.ID]
	if ok {
		st, _, _ := existing.Snapshot()
		if st == StatusRunning || st == StatusStarting {
			s.mu.Unlock()
			return fmt.Errorf("supervisor: project %s already %s", p.ID, st)
		}
	}
	proc := &Proc{
		ProjectID:   p.ID,
		Status:      StatusStarting,
		logs:        newRingBuffer(1000),
		subscribers: map[chan string]struct{}{},
		stopCh:      make(chan struct{}),
	}
	s.procs[p.ID] = proc
	s.mu.Unlock()
	return s.spawn(p, proc)
}

// spawn execs the child process and wires log capture + supervision.
func (s *Supervisor) spawn(project *Project, proc *Proc) error {
	cmd := exec.Command(s.binaryPath, "serve", project.ConfigPath())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		proc.Status = StatusStopped
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		proc.Status = StatusStopped
		return err
	}
	if err := cmd.Start(); err != nil {
		proc.Status = StatusStopped
		return err
	}
	proc.mu.Lock()
	proc.cmd = cmd
	proc.PID = cmd.Process.Pid
	proc.Status = StatusRunning
	proc.StartedAt = time.Now()
	proc.mu.Unlock()

	go s.pump(proc, stdout, "stdout")
	go s.pump(proc, stderr, "stderr")

	go s.wait(project, proc)
	return nil
}

// pump reads lines from r, appends to ring buffer, fans out to subscribers.
func (s *Supervisor) pump(proc *Proc, r io.Reader, stream string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		proc.logs.append(line)
		proc.subMu.Lock()
		for ch := range proc.subscribers {
			select {
			case ch <- line:
			default:
				// slow subscriber — drop
			}
		}
		proc.subMu.Unlock()
	}
}

// wait blocks for the child to exit, then handles status + auto-restart.
func (s *Supervisor) wait(project *Project, proc *Proc) {
	err := proc.cmd.Wait()
	proc.mu.Lock()
	wasStopping := proc.Status == StatusStopping
	stopCh := proc.stopCh
	if wasStopping {
		proc.Status = StatusStopped
		proc.PID = 0
		proc.mu.Unlock()
		return
	}
	if err != nil {
		proc.Status = StatusCrashed
	} else {
		proc.Status = StatusStopped
	}
	proc.PID = 0
	proc.mu.Unlock()

	// auto-restart on crash: max 3 restarts in 60s
	if err == nil {
		return
	}
	proc.mu.Lock()
	now := time.Now()
	cutoff := now.Add(-60 * time.Second)
	pruned := proc.restartWindow[:0]
	for _, t := range proc.restartWindow {
		if t.After(cutoff) {
			pruned = append(pruned, t)
		}
	}
	proc.restartWindow = pruned
	if len(proc.restartWindow) >= 3 {
		proc.mu.Unlock()
		return // give up
	}
	proc.restartWindow = append(proc.restartWindow, now)
	proc.Restarts++
	proc.mu.Unlock()

	// re-spawn after a short backoff, unless explicitly stopped meanwhile
	select {
	case <-stopCh:
		return
	case <-time.After(500 * time.Millisecond):
	}
	proc.mu.Lock()
	proc.Status = StatusStarting
	proc.mu.Unlock()
	if err := s.spawn(project, proc); err != nil {
		proc.mu.Lock()
		proc.Status = StatusCrashed
		proc.mu.Unlock()
	}
}

// Stop sends SIGTERM, then SIGKILL after 10s if still alive.
func (s *Supervisor) Stop(projectID string) error {
	s.mu.RLock()
	proc, ok := s.procs[projectID]
	s.mu.RUnlock()
	if !ok {
		return fmt.Errorf("supervisor: project %s not running", projectID)
	}
	proc.mu.Lock()
	if proc.Status != StatusRunning && proc.Status != StatusStarting {
		proc.mu.Unlock()
		return fmt.Errorf("supervisor: project %s not running", projectID)
	}
	proc.Status = StatusStopping
	close(proc.stopCh)
	proc.stopCh = make(chan struct{}) // reset for next start
	cmd := proc.cmd
	proc.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)

	done := make(chan struct{})
	go func() {
		_, _ = cmd.Process.Wait()
		close(done)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	select {
	case <-done:
	case <-ctx.Done():
		_ = cmd.Process.Kill()
	}
	return nil
}

// Restart stops then starts the project.
func (s *Supervisor) Restart(projectID string, p *Project) error {
	if proc, ok := s.Status(projectID); ok {
		st, _, _ := proc.Snapshot()
		if st == StatusRunning || st == StatusStarting {
			if err := s.Stop(projectID); err != nil {
				return err
			}
		}
	}
	return s.Start(p)
}

// Subscribe registers a channel that receives every new log line.
// Returns the channel, an unsubscribe func, and any error. The
// initial ringbuffer snapshot is sent on the channel synchronously.
func (s *Supervisor) Subscribe(projectID string) (<-chan string, func(), error) {
	s.mu.RLock()
	proc, ok := s.procs[projectID]
	s.mu.RUnlock()
	if !ok {
		return nil, nil, fmt.Errorf("supervisor: project %s has no proc", projectID)
	}
	ch := make(chan string, 256)

	// snapshot first
	for _, line := range proc.logs.snapshot() {
		select {
		case ch <- line:
		default:
		}
	}

	proc.subMu.Lock()
	proc.subscribers[ch] = struct{}{}
	proc.subMu.Unlock()
	cancel := func() {
		proc.subMu.Lock()
		delete(proc.subscribers, ch)
		proc.subMu.Unlock()
		close(ch)
	}
	return ch, cancel, nil
}

// StopAll stops every supervised process.
func (s *Supervisor) StopAll() {
	s.mu.RLock()
	ids := make([]string, 0, len(s.procs))
	for id := range s.procs {
		ids = append(ids, id)
	}
	s.mu.RUnlock()
	for _, id := range ids {
		_ = s.Stop(id)
	}
}

// ─── ringBuffer ──────────────────────────────────────────────────────

type ringBuffer struct {
	mu   sync.Mutex
	buf  []string
	head int
	size int
	cap  int
}

func newRingBuffer(capacity int) *ringBuffer {
	return &ringBuffer{buf: make([]string, capacity), cap: capacity}
}

func (r *ringBuffer) append(s string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buf[r.head] = s
	r.head = (r.head + 1) % r.cap
	if r.size < r.cap {
		r.size++
	}
}

func (r *ringBuffer) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, r.size)
	start := (r.head - r.size + r.cap) % r.cap
	for i := 0; i < r.size; i++ {
		out[i] = r.buf[(start+i)%r.cap]
	}
	return out
}
