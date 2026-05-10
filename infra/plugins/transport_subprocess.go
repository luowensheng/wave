package plugins

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// subprocessClient spawns the configured command for every Call,
// writes the JSON Request to stdin, reads JSON Response from stdout.
// Plugin lifetime = single request; transports keep no daemon process.
type subprocessClient struct {
	cfg *PluginConfig
}

func newSubprocessClient(cfg *PluginConfig) Client {
	return &subprocessClient{cfg: cfg}
}

func (c *subprocessClient) Close() error { return nil }

func (c *subprocessClient) Call(ctx context.Context, req *Request) (*Response, error) {
	ctx, cancel := context.WithTimeout(ctx, c.cfg.timeoutDuration())
	defer cancel()

	parts, err := splitCommand(c.cfg.Command)
	if err != nil {
		return nil, err
	}
	if len(parts) == 0 {
		return nil, fmt.Errorf("empty plugin command")
	}

	cmd := exec.CommandContext(ctx, parts[0], parts[1:]...)
	cmd.Env = mergeEnv(c.cfg.Env)

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}
	cmd.Stdin = bytes.NewReader(body)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("plugin exec: %w (stderr=%s)", err, strings.TrimSpace(stderr.String()))
	}

	var resp Response
	out := bytes.TrimSpace(stdout.Bytes())
	if len(out) == 0 {
		return nil, fmt.Errorf("plugin produced empty stdout")
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return nil, fmt.Errorf("decode response: %w (stdout=%s)", err, string(out))
	}
	if resp.Status == 0 {
		resp.Status = 200
	}
	return &resp, nil
}

// mergeEnv overlays cfg.Env on top of the parent process environment.
// Lets plugins inherit PATH etc. while still receiving config-supplied vars.
func mergeEnv(extra map[string]string) []string {
	env := os.Environ()
	for k, v := range extra {
		env = append(env, k+"="+v)
	}
	return env
}

// splitCommand is a minimal shell-like splitter that supports double quotes.
// Avoids pulling in a shell dependency while still letting users write
// `command: "./plugin --mode=verify"`.
func splitCommand(s string) ([]string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, fmt.Errorf("empty command")
	}
	var (
		out     []string
		current strings.Builder
		inQuote bool
	)
	flush := func() {
		if current.Len() > 0 {
			out = append(out, current.String())
			current.Reset()
		}
	}
	for _, r := range s {
		switch {
		case r == '"':
			inQuote = !inQuote
		case r == ' ' && !inQuote:
			flush()
		default:
			current.WriteRune(r)
		}
	}
	flush()
	if inQuote {
		return nil, fmt.Errorf("unclosed quote in command")
	}
	return out, nil
}
