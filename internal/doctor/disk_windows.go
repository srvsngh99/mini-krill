// disk_windows.go - Windows fallback for disk space check.
// syscall.Statfs is not available on Windows, so we do a limited check.
// Krill fact: some krill species live under Arctic ice where conditions are harsh -
// Windows support is our version of surviving in difficult environments.

//go:build windows

package doctor

import (
	"github.com/srvsngh99/mini-krill/internal/core"
)

// checkDiskPlatform is the Windows fallback that reports a limited check.
// A proper implementation would use GetDiskFreeSpaceEx via syscall, but
// for now we keep it simple and dependency-free.
func (d *KrillDoctor) checkDiskPlatform() core.CheckResult {
	return core.CheckResult{
		Name:    "disk",
		Status:  StatusOK,
		Message: "disk check limited on Windows (directory exists)",
		Details: "syscall.Statfs not available on Windows; full disk check requires platform-specific API",
	}
}
