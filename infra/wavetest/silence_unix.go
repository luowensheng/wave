//go:build unix

package wavetest

import (
	"os"
	"syscall"
)

// silenceOutput redirects fd 1 (stdout) and fd 2 (stderr) to
// /dev/null via syscall.Dup2 so the server's boot prints + access
// logs + stdlib `log` output (which caches os.Stderr at init) are
// all silenced for the duration of a test run. Returns a function
// that restores both fds.
//
// Unix-only — on Windows this is a no-op (use `--verbose` to debug
// suite failures there).
func silenceOutput() func() {
	devnull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return func() {}
	}

	oldStdout, err1 := syscall.Dup(int(os.Stdout.Fd()))
	oldStderr, err2 := syscall.Dup(int(os.Stderr.Fd()))
	if err1 != nil || err2 != nil {
		_ = devnull.Close()
		return func() {}
	}

	if err := syscall.Dup2(int(devnull.Fd()), int(os.Stdout.Fd())); err != nil {
		_ = syscall.Close(oldStdout)
		_ = syscall.Close(oldStderr)
		_ = devnull.Close()
		return func() {}
	}
	if err := syscall.Dup2(int(devnull.Fd()), int(os.Stderr.Fd())); err != nil {
		_ = syscall.Dup2(oldStdout, int(os.Stdout.Fd()))
		_ = syscall.Close(oldStdout)
		_ = syscall.Close(oldStderr)
		_ = devnull.Close()
		return func() {}
	}
	_ = devnull.Close() // dup2'd, original fd no longer needed

	return func() {
		_ = syscall.Dup2(oldStdout, int(os.Stdout.Fd()))
		_ = syscall.Dup2(oldStderr, int(os.Stderr.Fd()))
		_ = syscall.Close(oldStdout)
		_ = syscall.Close(oldStderr)
	}
}
