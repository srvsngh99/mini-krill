//go:build windows

package llm

import "os/exec"

// setProcessGroup is a no-op on Windows: there is no POSIX process group, the
// target CLIs are POSIX, and the shell-based runCLI tests skip on Windows.
func setProcessGroup(cmd *exec.Cmd) {}

// killProcessTree falls back to killing the direct child on Windows.
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}
