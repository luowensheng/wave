//go:build windows

package wavetest

// silenceOutput is a no-op on Windows. The Unix implementation uses
// syscall.Dup2 which doesn't have a clean equivalent on Windows;
// running `wave test --verbose` on Windows shows everything.
func silenceOutput() func() { return func() {} }
