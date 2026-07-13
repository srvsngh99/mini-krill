// Package tui implements the Terminal UI for mini-krill using Bubble Tea.
// Brand: a sanctioned "sonar" colour kit — mono house chrome (ink/paper/dim)
// with ONE owned accent (sonar) reserved for the symbol + active state, and
// functional state colour (OK / warn / error) only. See sai-brand-kit/mini-krill.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"

	"github.com/srvsngh99/mini-krill/internal/brand"
)

// ---------------------------------------------------------------------------
// Palette — mono house chrome + the sonar accent + functional state colours
// ---------------------------------------------------------------------------

const (
	// mini-krill is a sanctioned "sonar" colour kit: mono chrome + ONE owned
	// accent (sonar) on the symbol + active state; functional state colour only.
	ColorOceanBg   = lipgloss.Color("#161310") // house dark ground
	ColorCyan      = lipgloss.Color("#1fb8c9") // SONAR — the one owned accent (symbol + active state)
	ColorLightBlue = lipgloss.Color("#cfc9bd") // dim paper (secondary text)
	ColorGreen     = lipgloss.Color("#3fae6a") // functional — OK / live
	ColorAmber     = lipgloss.Color("#c08a2e") // functional — warn / idle
	ColorCoral     = lipgloss.Color("#cf5a4e") // functional — error / down
	ColorDimBlue   = lipgloss.Color("#3a3733") // borders — dim
	ColorMuted     = lipgloss.Color("#8a857c") // muted text
	ColorWhite     = lipgloss.Color("#d8d3ca") // bright text (paper)
)

// ---------------------------------------------------------------------------
// Lipgloss styles
// ---------------------------------------------------------------------------

var (
	// HeaderStyle renders the banner mark — the symbol carries the sonar accent.
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorCyan).
			Padding(0, 1)

	// TabStyle for inactive tabs in the tab bar (muted, mono).
	TabStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(ColorDimBlue).
			Padding(0, 2)

	// ActiveTabStyle — active state carries the sonar accent.
	ActiveTabStyle = lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(ColorCyan).
			Padding(0, 2)

	// UserBubbleStyle for user messages (mono; distinguished by alignment).
	UserBubbleStyle = lipgloss.NewStyle().
			Foreground(ColorWhite).
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorDimBlue).
			Padding(0, 1).
			MarginLeft(4)

	// KrillBubbleStyle for krill messages (mono; distinguished by alignment).
	KrillBubbleStyle = lipgloss.NewStyle().
				Foreground(ColorWhite).
				Border(lipgloss.NormalBorder()).
				BorderForeground(ColorDimBlue).
				Padding(0, 1).
				MarginRight(4)

	// StatusOK renders a LIVE badge (functional green).
	StatusOK = lipgloss.NewStyle().
			Foreground(ColorGreen).
			Bold(true)

	// StatusWarn renders an IDLE badge (functional amber).
	StatusWarn = lipgloss.NewStyle().
			Foreground(ColorAmber).
			Bold(true)

	// StatusFail renders a DOWN badge (functional red).
	StatusFail = lipgloss.NewStyle().
			Foreground(ColorCoral).
			Bold(true)

	// FooterStyle for the bottom status bar (muted, mono).
	FooterStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(ColorDimBlue).
			Padding(0, 1)

	// HelpKeyStyle for keyboard shortcut keys (mono bold).
	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorWhite).
			Bold(true)

	// HelpDescStyle for keyboard shortcut descriptions.
	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// TitleStyle for section titles (mono bold).
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorWhite).
			Bold(true)

	// ErrorStyle for error messages (functional red).
	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorCoral)

	// BoxStyle for dashboard panels (sharp border).
	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorDimBlue).
			Padding(1, 2)

	// BoxTitleStyle for dashboard panel titles (mono bold).
	BoxTitleStyle = lipgloss.NewStyle().
			Foreground(ColorWhite).
			Bold(true).
			Padding(0, 1).
			MarginBottom(1)

	// ValueStyle for dashboard values.
	ValueStyle = lipgloss.NewStyle().
			Foreground(ColorWhite)

	// LabelStyle for dashboard labels (muted, mono).
	LabelStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// InputStyle — the active input border carries the sonar accent.
	InputStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder()).
			BorderForeground(ColorCyan).
			Padding(0, 1)

	// DimStyle for secondary/muted text.
	DimStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// AccentStyle for highlighted text (the sonar accent).
	AccentStyle = lipgloss.NewStyle().
			Foreground(ColorCyan)

	// BrandStyle for Sourav AI Labs attribution (muted, mono).
	BrandStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)
)

// ---------------------------------------------------------------------------
// ASCII art and rendering
// ---------------------------------------------------------------------------

// RenderCompactHeader renders a single-line subtle branding bar for chat mode.
func RenderCompactHeader(version string, width int) string {
	left := HeaderStyle.Render(fmt.Sprintf("%s v%s", brand.Name, version))
	right := DimStyle.Render(brand.Attribution)

	leftW := lipgloss.Width(left)
	rightW := lipgloss.Width(right)
	gap := width - leftW - rightW - 2
	if gap < 1 {
		return left
	}

	return left + strings.Repeat(" ", gap) + right
}

// RenderHeader renders the full header banner with the ASCII krill logo.
func RenderHeader(version string, width int) string {
	compact := width > 0 && width < 72
	markLen := len(brand.Mark)
	if compact {
		markLen = len(brand.MarkCompact)
	}

	lines := brand.BannerLines(version, compact)

	var styled []string
	for i, line := range lines {
		switch {
		case i < markLen:
			styled = append(styled, HeaderStyle.Render(line))
		case strings.Contains(line, brand.Studio):
			styled = append(styled, BrandStyle.Bold(true).Padding(0, 1).Render(line))
		default:
			styled = append(styled, DimStyle.Padding(0, 1).Render(line))
		}
	}

	return strings.Join(styled, "\n")
}

// RenderStatus returns a styled status badge based on the status string.
// Accepts "ok"/"live", "warn"/"idle", or anything else (treated as down).
func RenderStatus(status string) string {
	switch strings.ToLower(status) {
	case "ok", "live", "healthy":
		return StatusOK.Render("[LIVE]")
	case "warn", "idle", "degraded":
		return StatusWarn.Render("[IDLE]")
	default:
		return StatusFail.Render("[DOWN]")
	}
}

// RenderKeyValue renders a label-value pair for dashboard panels.
func RenderKeyValue(label, value string) string {
	return LabelStyle.Render(label+": ") + ValueStyle.Render(value)
}

// RenderBox renders content inside a titled bordered box.
func RenderBox(title, content string, width int) string {
	t := BoxTitleStyle.Render(title)
	body := BoxStyle.Width(width).Render(content)
	return lipgloss.JoinVertical(lipgloss.Left, t, body)
}
