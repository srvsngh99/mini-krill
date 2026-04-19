// disk_unix.go - Unix/macOS disk space check using syscall.Statfs.

//go:build !windows

package doctor

import (
	"fmt"
	"syscall"

	"github.com/srvsngh99/mini-krill/internal/core"
)

// checkDiskPlatform checks free disk space on Unix/macOS using syscall.Statfs.
func (d *KrillDoctor) checkDiskPlatform() core.CheckResult {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(d.brainDir, &stat); err != nil {
		return core.CheckResult{
			Name:    "disk",
			Status:  StatusFail,
			Message: "failed to check disk space",
			Details: err.Error(),
		}
	}

	// Available space for non-root users
	freeBytes := stat.Bavail * uint64(stat.Bsize)
	totalBytes := stat.Blocks * uint64(stat.Bsize)

	freeMB := float64(freeBytes) / (1024 * 1024)
	totalGB := float64(totalBytes) / (1024 * 1024 * 1024)
	details := fmt.Sprintf("free=%.0f MB, total=%.1f GB, path=%s", freeMB, totalGB, d.brainDir)

	if freeBytes < diskFailThreshold {
		return core.CheckResult{
			Name:    "disk",
			Status:  StatusFail,
			Message: fmt.Sprintf("critically low disk space: %.0f MB free", freeMB),
			Details: details,
		}
	}

	if freeBytes < diskWarnThreshold {
		return core.CheckResult{
			Name:    "disk",
			Status:  StatusWarn,
			Message: fmt.Sprintf("low disk space: %.0f MB free", freeMB),
			Details: details,
		}
	}

	return core.CheckResult{
		Name:    "disk",
		Status:  StatusOK,
		Message: fmt.Sprintf("disk space adequate: %.0f MB free", freeMB),
		Details: details,
	}
}
