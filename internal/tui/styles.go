// Package tui implements the Terminal UI for Mini Krill using Bubble Tea.
// The ocean-themed interface is the showcase of the project - gorgeous,
// functional, and full of crustaceous personality.
package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// ---------------------------------------------------------------------------
// Krill-themed color palette
// ---------------------------------------------------------------------------

const (
	ColorOceanBg  = lipgloss.Color("#0a1628") // deep ocean background
	ColorCyan     = lipgloss.Color("#00d4ff") // primary - sonar pulse
	ColorLightBlue = lipgloss.Color("#7ec8e3") // secondary - shallow waters
	ColorGreen    = lipgloss.Color("#00ff88") // bioluminescent accent
	ColorAmber    = lipgloss.Color("#ffaa00") // warning - surface light
	ColorCoral    = lipgloss.Color("#ff6b6b") // error - coral reef
	ColorDimBlue  = lipgloss.Color("#1e3a5f") // borders - twilight zone
	ColorMuted    = lipgloss.Color("#6b7b8d") // muted text - deep silt
	ColorWhite    = lipgloss.Color("#e8f4f8") // bright text - foam
)

// ---------------------------------------------------------------------------
// Lipgloss styles
// ---------------------------------------------------------------------------

var (
	// HeaderStyle for the top banner area.
	HeaderStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(ColorCyan).
			Padding(0, 1)

	// TabStyle for inactive tabs in the tab bar.
	TabStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(ColorDimBlue).
			Padding(0, 2)

	// ActiveTabStyle for the currently selected tab.
	ActiveTabStyle = lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true).
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(ColorCyan).
			Padding(0, 2)

	// UserBubbleStyle for user messages in chat (right-aligned feel).
	UserBubbleStyle = lipgloss.NewStyle().
			Foreground(ColorLightBlue).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDimBlue).
			Padding(0, 1).
			MarginLeft(4)

	// KrillBubbleStyle for krill messages in chat (left-aligned).
	KrillBubbleStyle = lipgloss.NewStyle().
			Foreground(ColorGreen).
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDimBlue).
			Padding(0, 1).
			MarginRight(4)

	// StatusOK renders a green LIVE badge.
	StatusOK = lipgloss.NewStyle().
			Foreground(ColorGreen).
			Bold(true)

	// StatusWarn renders an amber IDLE badge.
	StatusWarn = lipgloss.NewStyle().
			Foreground(ColorAmber).
			Bold(true)

	// StatusFail renders a coral DOWN badge.
	StatusFail = lipgloss.NewStyle().
			Foreground(ColorCoral).
			Bold(true)

	// FooterStyle for the bottom status bar.
	FooterStyle = lipgloss.NewStyle().
			Foreground(ColorMuted).
			Border(lipgloss.NormalBorder(), true, false, false, false).
			BorderForeground(ColorDimBlue).
			Padding(0, 1)

	// HelpKeyStyle for keyboard shortcut keys in help.
	HelpKeyStyle = lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true)

	// HelpDescStyle for keyboard shortcut descriptions.
	HelpDescStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// TitleStyle for section titles.
	TitleStyle = lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true)

	// ErrorStyle for error messages.
	ErrorStyle = lipgloss.NewStyle().
			Foreground(ColorCoral)

	// BoxStyle for dashboard panels.
	BoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorDimBlue).
			Padding(1, 2)

	// BoxTitleStyle for dashboard panel titles.
	BoxTitleStyle = lipgloss.NewStyle().
			Foreground(ColorCyan).
			Bold(true).
			Padding(0, 1).
			MarginBottom(1)

	// ValueStyle for dashboard values.
	ValueStyle = lipgloss.NewStyle().
			Foreground(ColorWhite)

	// LabelStyle for dashboard labels.
	LabelStyle = lipgloss.NewStyle().
			Foreground(ColorLightBlue)

	// InputStyle for the chat input field.
	InputStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(ColorCyan).
			Padding(0, 1)

	// DimStyle for secondary/muted text.
	DimStyle = lipgloss.NewStyle().
			Foreground(ColorMuted)

	// AccentStyle for highlighted text.
	AccentStyle = lipgloss.NewStyle().
			Foreground(ColorGreen)
)

// ---------------------------------------------------------------------------
// ASCII art and rendering
// ---------------------------------------------------------------------------

// RenderHeader renders the full header banner with the ASCII krill logo.
func RenderHeader(version string) string {
	lines := []string{
		"   ~~~~",
		fmt.Sprintf("  >=\\'>    Mini Krill v%s", version),
		"   ~~~~    Your crustaceous AI buddy",
	}

	var styled []string
	for _, line := range lines {
		styled = append(styled, HeaderStyle.Render(line))
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
