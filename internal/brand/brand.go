// Package brand centralizes Mini Krill's public identity and terminal marks.
package brand

import "fmt"

const (
	Name        = "Mini Krill"
	Studio      = "Sourav AI Labs"
	Creator     = "Sourav Singh"
	Attribution = "by Sourav Singh / Sourav AI Labs"
	Tagline     = "Local-first AI agent with a crustaceous soul"
)

// Mark is an ASCII interpretation of the circular Mini Krill logo.
// It intentionally stays plain ASCII so it renders in conservative terminals.
var Mark = []string{
	"        .-''''''''-.",
	"     .-'   .----.   '-.",
	"   .'    .'  __  '.    '.",
	"  /     /  .'oo'.  \\     \\",
	" ;     |  /_____)   |     ;",
	" |     |   / / /    |     |",
	" ;     |  /_/ /__   |     ;",
	"  \\     \\    '--'  /     /",
	"   '.    '._    _.'    .'",
	"     '-.     '''     .-'",
	"        '-.______.-'",
}

// MarkCompact is for narrow terminals and short CLI banners.
var MarkCompact = []string{
	"   .-''''-.",
	"  /  >o  /)",
	" |  /___/ |",
	"  \\__\\_\\_/",
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
	)
	return lines
}
