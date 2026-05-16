//go:build !windows

package llm

import (
	"os/exec"
	"syscall"
)

// setProcessGroup makes the child the leader of a new process group, so the
// whole subprocess tree can be signalled at once. The target Codex/Claude
// CLIs (and `sh -c` in the tests) fork helper grandchildren; without this,
// exec.CommandContext SIGKILLs only the direct child, the orphaned
// grandchildren keep the stdout/stderr pipe write ends open, the reader
// goroutines never EOF, and the idle-kill path hangs on <-done until the
// grandchild exits on its own — the round-4 CI regression.
func setProcessGroup(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// killProcessTree SIGKILLs the entire process group led by cmd's child. With
// Setpgid the child's PGID equals its PID, so -PID addresses the whole group
// (children and grandchildren included). Safe to call repeatedly and after
// the process has already exited: a vanished group yields ESRCH, which is
// ignored. Falls back to killing just the direct child if the group signal
// fails for any other reason.
func killProcessTree(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return nil
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
		return cmd.Process.Kill()
	}
	return nil
}
