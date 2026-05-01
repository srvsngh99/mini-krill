package plugin

import (
	"embed"
	"io/fs"
	"os"
	"path/filepath"

	log "github.com/srvsngh99/mini-krill/internal/log"
)

//go:embed defaults/*.yaml
var defaultSkillFiles embed.FS

// SeedDefaultSkills writes bundled YAML skill files to the skills directory
// if they don't already exist. User edits to existing files are preserved.
func SeedDefaultSkills(dir string) {
	if dir == "" {
		return
	}

	entries, err := fs.ReadDir(defaultSkillFiles, "defaults")
	if err != nil {
		log.Debug("could not read embedded default skills", "error", err)
		return
	}

	_ = os.MkdirAll(dir, 0755)

	seeded := 0
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		dest := filepath.Join(dir, entry.Name())

		// Don't overwrite user edits
		if _, err := os.Stat(dest); err == nil {
			continue
		}

		data, err := defaultSkillFiles.ReadFile("defaults/" + entry.Name())
		if err != nil {
			log.Warn("could not read embedded skill file", "name", entry.Name(), "error", err)
			continue
		}

		if err := os.WriteFile(dest, data, 0644); err != nil {
			log.Warn("could not write default skill", "path", dest, "error", err)
			continue
		}
		seeded++
	}

	if seeded > 0 {
		log.Info("seeded default skills", "dir", dir, "count", seeded)
	}
}
