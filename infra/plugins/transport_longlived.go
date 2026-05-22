package plugins

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/luowensheng/wave/infra/logger"
)

// longLivedClient is a JSON-RPC 2.0 transport that keeps one subprocess
// alive for the whole life of the registered plugin.
//
// Wire format: each request and response is a single newline-terminated
// JSON object on stdin/stdout. Stderr is captured and forwarded to slog.
//
// Concurrency: the write side is mutex-serialized; in-flight calls are
// keyed by request id and resolved by a single reader goroutine.
//
// Lifecycle: the process is started lazily on the first Call. On Close
// we send the JSON-RPC `shutdown` notification, then SIGTERM after 2s,
// then SIGKILL after a further 5s.
//
// Restart policy: if the subprocess exits unexpectedly we bring it back
// up on the next Call. If we see more than 5 restarts in any 1-minute
// window we mark the plugin "dead" and fail every subsequent Call.
type longLivedClient struct {
	cfg  *PluginConfig
	name string

	mu         sync.Mutex // protects proc, in-flight map, write side, restart bookkeeping
	proc       *llProc
	nextID     uint64
	closed     bool
	dead       bool
	restartsAt []time.Time
}

// llProc holds the bookkeeping for one running subprocess.
type llProc struct {
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	stdout   io.ReadCloser
	stderr   io.ReadCloser
	writeMu  sync.Mutex
	pending  sync.Map // map[uint64]chan *rpcResponse
	exitErr  atomic.Value // error
	exitedCh chan struct{}
}

func newLongLivedClient(cfg *PluginConfig, name string) Client {
	return &longLivedClient{cfg: cfg, name: name}
}

// Close terminates the subprocess (if any) gracefully.
func (c *longLivedClient) Close() error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	p := c.proc
	c.proc = nil
	c.mu.Unlock()
	if p == nil {
		return nil
	}
	return shutdownProc(p)
}

// Call routes through RPC using the legacy "handler.call" method so a
// long-lived plugin can also serve handler-shaped requests if desired.
func (c *longLivedClient) Call(ctx context.Context, req *Request) (*Response, error) {
	raw, err := c.RPC(ctx, "handler.call", req)
	if err != nil {
		return nil, err
	}
	var resp Response
	if err := json.Unmarshal(raw, &resp); err != nil {
		return nil, fmt.Errorf("decode handler response: %w", err)
	}
	if resp.Status == 0 {
		resp.Status = 200
	}
	return &resp, nil
}

// RPC marshals params, writes a JSON-RPC 2.0 request to stdin, and
// blocks until the matching response arrives or ctx is cancelled.
func (c *longLivedClient) RPC(ctx context.Context, method string, params any) (json.RawMessage, error) {
	p, id, err := c.acquire()
	if err != nil {
		return nil, err
	}
	rawParams, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("marshal params: %w", err)
	}
	respCh := make(chan *rpcResponse, 1)
	p.pending.Store(id, respCh)
	defer p.pending.Delete(id)

	idCopy := id
	req := rpcRequest{JSONRPC: "2.0", ID: &idCopy, Method: method, Params: rawParams}
	if err := writeFrame(p, req); err != nil {
		return nil, fmt.Errorf("write rpc: %w", err)
	}

	timeout := c.cfg.timeoutDuration()
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-timer.C:
		return nil, fmt.Errorf("plugin %q: rpc %q timed out after %s", c.name, method, timeout)
	case <-p.exitedCh:
		if v := p.exitErr.Load(); v != nil {
			if e, ok := v.(error); ok && e != nil {
				return nil, fmt.Errorf("plugin %q exited mid-call: %w", c.name, e)
			}
		}
		return nil, fmt.Errorf("plugin %q exited mid-call", c.name)
	case resp := <-respCh:
		if resp.Error != nil {
			return nil, resp.Error
		}
		return resp.Result, nil
	}
}

// acquire returns the active subprocess (starting one if needed) and a
// fresh request id.
func (c *longLivedClient) acquire() (*llProc, uint64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return nil, 0, fmt.Errorf("plugin %q: client is closed", c.name)
	}
	if c.dead {
		return nil, 0, fmt.Errorf("plugin %q: marked dead after repeated restarts", c.name)
	}
	if c.proc == nil {
		// If we are restarting after an exit, record it for backoff bookkeeping.
		if !c.recordRestart() {
			c.dead = true
			return nil, 0, fmt.Errorf("plugin %q: restart loop detected (>5 in 1min)", c.name)
		}
		p, err := c.startProc()
		if err != nil {
			return nil, 0, err
		}
		c.proc = p
	}
	c.nextID++
	return c.proc, c.nextID, nil
}

// recordRestart appends a timestamp and returns false if the restart
// budget for the rolling 1-minute window is exhausted.
//
// First start counts as a restart so a deliberately broken plugin
// can't loop forever; the budget is generous enough (5 starts/min)
// that legitimate boot patterns aren't affected.
func (c *longLivedClient) recordRestart() bool {
	now := time.Now()
	cutoff := now.Add(-1 * time.Minute)
	kept := c.restartsAt[:0]
	for _, t := range c.restartsAt {
		if t.After(cutoff) {
			kept = append(kept, t)
		}
	}
	kept = append(kept, now)
	c.restartsAt = kept
	return len(kept) <= 5
}

func (c *longLivedClient) startProc() (*llProc, error) {
	parts, err := splitCommand(c.cfg.Command)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty plugin command")
	}
	cmd := exec.Command(parts[0], parts[1:]...)
	cmd.Env = mergeEnv(c.cfg.Env)

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("stdin pipe: %w", err)
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("stderr pipe: %w", err)
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start plugin %q: %w", c.name, err)
	}
	p := &llProc{
		cmd:      cmd,
		stdin:    stdin,
		stdout:   stdout,
		stderr:   stderr,
		exitedCh: make(chan struct{}),
	}
	go p.readLoop(c.name)
	go p.stderrLoop(c.name)
	go p.waitLoop(c)
	return p, nil
}

// onUnexpectedExit is invoked from the wait goroutine when the process
// dies. It clears c.proc so the next Call triggers a restart and resolves
// every pending request with an error so callers don't hang.
func (c *longLivedClient) onUnexpectedExit(p *llProc) {
	c.mu.Lock()
	if c.proc == p {
		c.proc = nil
	}
	c.mu.Unlock()
	p.pending.Range(func(_, v any) bool {
		if ch, ok := v.(chan *rpcResponse); ok {
			select {
			case ch <- &rpcResponse{Error: &rpcError{Code: -32000, Message: "plugin exited"}}:
			default:
			}
		}
		return true
	})
}

func (p *llProc) readLoop(name string) {
	scanner := bufio.NewScanner(p.stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var resp rpcResponse
		if err := json.Unmarshal(line, &resp); err != nil {
			logger.Slog.Warn("plugin: malformed rpc frame", "plugin", name, "err", err)
			continue
		}
		if resp.ID == nil {
			// Server-initiated notification — ignore for now.
			continue
		}
		v, ok := p.pending.Load(*resp.ID)
		if !ok {
			continue
		}
		if ch, ok := v.(chan *rpcResponse); ok {
			select {
			case ch <- &resp:
			default:
			}
		}
	}
	if err := scanner.Err(); err != nil && !errors.Is(err, io.EOF) {
		logger.Slog.Warn("plugin: stdout reader error", "plugin", name, "err", err)
	}
}

func (p *llProc) stderrLoop(name string) {
	scanner := bufio.NewScanner(p.stderr)
	scanner.Buffer(make([]byte, 16*1024), 1*1024*1024)
	for scanner.Scan() {
		logger.Slog.Info("plugin stderr", "plugin", name, "line", scanner.Text())
	}
}

func (p *llProc) waitLoop(c *longLivedClient) {
	err := p.cmd.Wait()
	if err != nil {
		p.exitErr.Store(err)
	}
	close(p.exitedCh)
	c.mu.Lock()
	closed := c.closed
	c.mu.Unlock()
	if closed {
		return
	}
	logger.Slog.Warn("plugin exited unexpectedly", "plugin", c.name, "err", err)
	c.onUnexpectedExit(p)
}

// writeFrame serializes a JSON-RPC frame and writes it to stdin under
// the per-process write mutex (so concurrent callers don't interleave).
func writeFrame(p *llProc, req rpcRequest) error {
	buf, err := json.Marshal(req)
	if err != nil {
		return err
	}
	buf = append(buf, '\n')
	p.writeMu.Lock()
	defer p.writeMu.Unlock()
	_, err = p.stdin.Write(buf)
	return err
}

// shutdownProc sends the shutdown notification, then escalates to
// SIGTERM and SIGKILL on the standard 2s/5s timeline.
func shutdownProc(p *llProc) error {
	notice := rpcRequest{JSONRPC: "2.0", Method: "shutdown"}
	_ = writeFrame(p, notice)
	_ = p.stdin.Close()

	select {
	case <-p.exitedCh:
		return nil
	case <-time.After(2 * time.Second):
	}
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
	}
	select {
	case <-p.exitedCh:
		return nil
	case <-time.After(5 * time.Second):
	}
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Kill()
	}
	<-p.exitedCh
	return nil
}
