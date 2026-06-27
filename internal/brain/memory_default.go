//go:build !colony

package brain

import (
	"github.com/srvsngh99/mini-krill/internal/config"
	"github.com/srvsngh99/mini-krill/internal/core"
)

// newDefaultMemory (public release) returns the original file-backed memory.
// The Chroma-backed memory is a colony-only deviation (see memory_chroma.go),
// so the public Mini-Krill binary carries no Chroma dependency.
func newDefaultMemory(memDir string, cfg config.BrainConfig) (core.Memory, error) {
	return NewFileMemory(memDir, cfg.MaxMemories)
}
