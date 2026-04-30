//go:build !windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func processRunning(proc *os.Process) bool {
	return proc.Signal(syscall.Signal(0)) == nil
}

func detachCommand(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
}

func terminationSignals() []os.Signal {
	return []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP}
}

func terminateProcess(proc *os.Process) error {
	return proc.Signal(syscall.SIGTERM)
}
