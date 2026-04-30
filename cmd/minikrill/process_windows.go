//go:build windows

package main

import (
	"os"
	"os/exec"
)

func processRunning(proc *os.Process) bool {
	return proc != nil
}

func detachCommand(_ *exec.Cmd) {}

func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func terminateProcess(proc *os.Process) error {
	return proc.Kill()
}
