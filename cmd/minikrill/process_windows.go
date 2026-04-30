//go:build windows

package main

import (
	"os"
	"os/exec"
	"syscall"
)

func processRunning(proc *os.Process) bool {
	if proc == nil {
		return false
	}
	const PROCESS_QUERY_LIMITED_INFORMATION = 0x1000
	handle, err := syscall.OpenProcess(PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(proc.Pid))
	if err != nil {
		return false
	}
	defer syscall.CloseHandle(handle)
	var exitCode uint32
	if err := syscall.GetExitCodeProcess(handle, &exitCode); err != nil {
		return false
	}
	const STILL_ACTIVE = 259
	return exitCode == STILL_ACTIVE
}

func detachCommand(_ *exec.Cmd) {}

func terminationSignals() []os.Signal {
	return []os.Signal{os.Interrupt}
}

func terminateProcess(proc *os.Process) error {
	return proc.Kill()
}
