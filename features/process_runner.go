package features

import "context"

// ProcessRunner is the capability of executing shell scripts/commands
// with platform-specific behavior, variable templating, and captured
// output. The orchestrator wires concrete closures backed by infra/exec.
type ProcessRunner struct {
	Run func(ctx context.Context, command string, args map[string]string) (stdout, stderr []byte, err error)
}
