// Package brand centralizes Mini Krill's public identity and terminal marks.
package brand

import "fmt"

const (
	Name        = "Mini Krill"
	Studio      = "Sourav AI Labs"
	Creator     = "Sourav Singh"
	Attribution = "by Sourav Singh / Sourav AI Labs"
	Tagline     = "Local-first AI agent with a crustaceous soul"
	Credits     = "Inspired by Jarvis and OpenClaw"

	// Terminal-prompt lockup marks used by the CLI banner (cmd/minikrill).
	// They render as:
	//   >_ mini-krill   v0.1.4
	//   > SAI_  Sourav AI Labs  ·  souravailabs.ai
	Wordmark = ">_ mini-krill"   // bold product wordmark
	LabMark  = "> SAI_"          // Sourav AI Labs terminal monogram
	Lab      = Studio            // "Sourav AI Labs"
	Site     = "souravailabs.ai" // public site
)

// Mark is an ASCII krill/shrimp silhouette inspired by the circular logo.
// Pure ASCII so it renders in conservative terminals.
var Mark = []string{
	"            .--._",
	"           /  o  \\__",
	"          |   __    '-.___",
	"          |  /  \\        '==-.",
	"           \\|    |  /\\/\\/\\   \\",
	"            '._  | |      |   |",
	"               '-| |  ))  |  /",
	"                 |  \\_/\\_/  /",
	"                  \\  '---'_/",
	"                   '-.___/",
}

// MarkCompact is for narrow terminals and short CLI banners.
var MarkCompact = []string{
	"    .--.__",
	"   / o    '=-.",
	"   \\| /\\/\\  /",
	"    '-'--'-'",
}

func BannerLines(version string, compact bool) []string {
	mark := Mark
	if compact {
		mark = MarkCompact
	}

	lines := append([]string{}, mark...)
	lines = append(lines,
		fmt.Sprintf("%s v%s", Name, version),
		Attribution,
		Tagline,
		Credits,
	)
	return lines
}
