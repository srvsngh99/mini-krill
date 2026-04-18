package tui

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// ---------------------------------------------------------------------------
// Chat entry
// ---------------------------------------------------------------------------

// chatEntry represents a single message in the chat history.
type chatEntry struct {
	sender string
	text   string
	isUser bool
}

// ---------------------------------------------------------------------------
// DashboardView
// ---------------------------------------------------------------------------

// DashboardView shows system status, active sub-krills, and a krill fact.
type DashboardView struct {
	status  core.HealthStatus
	fact    string
	version string
	width   int
	height  int
}

// NewDashboardView creates a dashboard with initial state.
func NewDashboardView(version string) DashboardView {
	return DashboardView{
		version: version,
		fact:    randomKrillFact(),
	}
}

// SetSize updates the view dimensions.
func (d *DashboardView) SetSize(w, h int) {
	d.width = w
	d.height = h
}

// Update processes messages for the dashboard.
func (d *DashboardView) Update(msg tea.Msg) tea.Cmd {
	return nil
}

// View renders the dashboard.
func (d *DashboardView) View() string {
	if d.width < 20 {
		return "Terminal too small..."
	}

	// Calculate panel widths - leave room for borders and padding
	panelWidth := (d.width - 6) / 2
	if panelWidth < 20 {
		panelWidth = d.width - 4
	}

	// --- System Status panel ---
	uptime := formatDuration(d.status.Uptime)
	memMB := float64(d.status.MemoryUsed) / (1024 * 1024)

	llmBadge := RenderStatus(d.status.LLMStatus)
	brainBadge := RenderStatus("down")
	if d.status.BrainOK {
		brainBadge = RenderStatus("ok")
	}
	aliveBadge := RenderStatus("down")
	if d.status.Alive {
		aliveBadge = RenderStatus("ok")
	}

	statusLines := []string{
		RenderKeyValue("Status", aliveBadge),
		RenderKeyValue("Uptime", uptime),
		RenderKeyValue("Memory", fmt.Sprintf("%.1f MB", memMB)),
		RenderKeyValue("LLM", llmBadge),
		RenderKeyValue("Brain", brainBadge),
		RenderKeyValue("Version", d.version),
	}
	statusContent := strings.Join(statusLines, "\n")
	statusBox := RenderBox("  System Status", statusContent, panelWidth)

	// --- Active Sub-Krills panel ---
	subKrillContent := DimStyle.Render("No active sub-krills\nThe swarm is resting...")
	subKrillBox := RenderBox("  Active Sub-Krills", subKrillContent, panelWidth)

	// --- Krill Fact panel ---
	factWrapped := wordWrap(d.fact, d.width-8)
	factContent := AccentStyle.Render("  " + factWrapped)
	factBox := RenderBox("  Did You Know?", factContent, d.width-4)

	// Layout: status + sub-krills side by side, fact below
	if panelWidth < d.width-4 {
		// Wide enough for side-by-side
		topRow := lipgloss.JoinHorizontal(lipgloss.Top, statusBox, "  ", subKrillBox)
		return lipgloss.JoinVertical(lipgloss.Left, topRow, "", factBox)
	}

	// Narrow: stack vertically
	return lipgloss.JoinVertical(lipgloss.Left, statusBox, "", subKrillBox, "", factBox)
}

// ---------------------------------------------------------------------------
// ChatView
// ---------------------------------------------------------------------------

// ChatView provides an interactive chat with the krill agent.
type ChatView struct {
	messages []chatEntry
	input    textinput.Model
	viewport viewport.Model
	width    int
	height   int
	agent    core.Agent
	waiting  bool
	ready    bool
}

// NewChatView creates a chat view with a text input and viewport.
func NewChatView(agent core.Agent) ChatView {
	ti := textinput.New()
	ti.Placeholder = "Talk to krill... (Enter to send)"
	ti.CharLimit = 2000
	ti.Width = 60
	ti.PromptStyle = lipgloss.NewStyle().Foreground(ColorCyan)
	ti.TextStyle = lipgloss.NewStyle().Foreground(ColorWhite)
	ti.PlaceholderStyle = lipgloss.NewStyle().Foreground(ColorMuted)
	ti.Cursor.Style = lipgloss.NewStyle().Foreground(ColorGreen)
	ti.Focus()

	vp := viewport.New(80, 20)
	vp.Style = lipgloss.NewStyle().Foreground(ColorWhite)

	greeting := chatEntry{
		sender: "krill",
		text:   "Hey there! I'm Mini Krill, your crustaceous AI buddy. I live in the deep ocean of your terminal. Ask me anything - I might be small, but I'm part of the largest biomass on Earth!",
		isUser: false,
	}

	return ChatView{
		messages: []chatEntry{greeting},
		input:    ti,
		viewport: vp,
		agent:    agent,
	}
}

// SetSize updates the chat view dimensions and recalculates layout.
func (c *ChatView) SetSize(w, h int) {
	c.width = w
	c.height = h

	// Input takes 3 lines (border + text + border), leave rest for viewport
	inputHeight := 3
	vpHeight := h - inputHeight - 1
	if vpHeight < 3 {
		vpHeight = 3
	}

	c.viewport.Width = w - 2
	c.viewport.Height = vpHeight
	c.input.Width = w - 6

	c.refreshViewport()
	c.ready = true
}

// Focused returns whether the chat input is focused.
func (c *ChatView) Focused() bool {
	return c.input.Focused()
}

// Focus activates the chat input.
func (c *ChatView) Focus() {
	c.input.Focus()
}

// Blur deactivates the chat input.
func (c *ChatView) Blur() {
	c.input.Blur()
}

// Update handles messages for the chat view.
func (c *ChatView) Update(msg tea.Msg) tea.Cmd {
	var cmds []tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "enter":
			if c.waiting {
				return nil
			}
			text := strings.TrimSpace(c.input.Value())
			if text == "" {
				return nil
			}

			// Add user message
			c.messages = append(c.messages, chatEntry{
				sender: "you",
				text:   text,
				isUser: true,
			})
			c.input.Reset()
			c.waiting = true
			c.refreshViewport()

			// Send to agent in goroutine
			return c.sendChat(text)
		}
	case chatResponseMsg:
		c.waiting = false
		response := msg.response
		if msg.err != nil {
			response = fmt.Sprintf("Oops, hit a reef: %v", msg.err)
			log.Error("chat response error", "error", msg.err)
		}
		c.messages = append(c.messages, chatEntry{
			sender: "krill",
			text:   response,
			isUser: false,
		})
		c.refreshViewport()
		return nil
	}

	// Forward to input and viewport
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	cmds = append(cmds, cmd)

	c.viewport, cmd = c.viewport.Update(msg)
	cmds = append(cmds, cmd)

	return tea.Batch(cmds...)
}

// View renders the chat view.
func (c *ChatView) View() string {
	if !c.ready {
		return "\n  Initializing chat..."
	}

	// Input area
	inputBox := InputStyle.Width(c.width - 4).Render(c.input.View())

	// Combine viewport and input
	return lipgloss.JoinVertical(
		lipgloss.Left,
		c.viewport.View(),
		inputBox,
	)
}

// refreshViewport rebuilds the viewport content from messages.
func (c *ChatView) refreshViewport() {
	var lines []string

	maxBubbleWidth := c.width - 12
	if maxBubbleWidth < 20 {
		maxBubbleWidth = 20
	}

	for _, msg := range c.messages {
		wrapped := wordWrap(msg.text, maxBubbleWidth)

		if msg.isUser {
			// Right-aligned user bubble
			bubble := UserBubbleStyle.MaxWidth(maxBubbleWidth + 4).Render(wrapped)
			// Right-align the bubble
			padded := lipgloss.NewStyle().Width(c.width - 4).Align(lipgloss.Right).Render(bubble)
			label := lipgloss.NewStyle().
				Width(c.width - 4).
				Align(lipgloss.Right).
				Foreground(ColorMuted).
				Render("you")
			lines = append(lines, label, padded, "")
		} else {
			// Left-aligned krill bubble
			bubble := KrillBubbleStyle.MaxWidth(maxBubbleWidth + 4).Render(wrapped)
			label := DimStyle.Render("  krill")
			lines = append(lines, label, "  "+bubble, "")
		}
	}

	// Waiting indicator
	if c.waiting {
		spinner := AccentStyle.Render("  ~ Krill is diving deep... ~")
		lines = append(lines, spinner)
	}

	content := strings.Join(lines, "\n")
	c.viewport.SetContent(content)
	c.viewport.GotoBottom()
}

// sendChat creates a command that calls the agent and returns the response.
func (c *ChatView) sendChat(text string) tea.Cmd {
	agent := c.agent
	return func() tea.Msg {
		if agent == nil {
			return chatResponseMsg{
				response: "No agent connected - I'm swimming solo right now. Try again after the LLM is configured!",
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		resp, err := agent.Chat(ctx, text)
		return chatResponseMsg{response: resp, err: err}
	}
}

// ---------------------------------------------------------------------------
// LogsView
// ---------------------------------------------------------------------------

// LogsView displays a scrollable view of log file contents.
type LogsView struct {
	viewport viewport.Model
	content  string
	logFile  string
	width    int
	height   int
	ready    bool
}

// NewLogsView creates a log viewer for the given file path.
func NewLogsView(logFile string) LogsView {
	vp := viewport.New(80, 20)
	vp.Style = lipgloss.NewStyle().Foreground(ColorWhite)

	return LogsView{
		viewport: vp,
		logFile:  logFile,
		content:  DimStyle.Render("Loading logs..."),
	}
}

// SetSize updates the log view dimensions.
func (l *LogsView) SetSize(w, h int) {
	l.width = w
	l.height = h
	l.viewport.Width = w - 2
	l.viewport.Height = h - 1
	l.ready = true
}

// RefreshLogs reads the log file and updates the viewport.
func (l *LogsView) RefreshLogs() {
	if l.logFile == "" {
		l.content = DimStyle.Render("No log file configured")
		l.viewport.SetContent(l.content)
		return
	}

	data, err := os.ReadFile(l.logFile)
	if err != nil {
		l.content = DimStyle.Render(fmt.Sprintf("  Cannot read log file: %v\n  Path: %s", err, l.logFile))
		l.viewport.SetContent(l.content)
		return
	}

	if len(data) == 0 {
		l.content = DimStyle.Render("  Log file is empty - krill is being quiet...")
		l.viewport.SetContent(l.content)
		return
	}

	// Show last portion of the log file
	text := string(data)
	logLines := strings.Split(text, "\n")

	// Keep last N lines to avoid overwhelming the viewport
	maxLines := l.height * 3
	if maxLines < 100 {
		maxLines = 100
	}
	if len(logLines) > maxLines {
		logLines = logLines[len(logLines)-maxLines:]
	}

	// Colorize log levels
	var styled []string
	for _, line := range logLines {
		styled = append(styled, colorizeLogLine(line))
	}

	l.content = strings.Join(styled, "\n")
	l.viewport.SetContent(l.content)
	l.viewport.GotoBottom()
}

// Update handles messages for the logs view.
func (l *LogsView) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	l.viewport, cmd = l.viewport.Update(msg)
	return cmd
}

// View renders the logs view.
func (l *LogsView) View() string {
	if !l.ready {
		return "\n  Initializing log viewer..."
	}

	header := DimStyle.Render(fmt.Sprintf("  Log: %s  (scroll with j/k or arrow keys)", l.logFile))
	return lipgloss.JoinVertical(lipgloss.Left, header, l.viewport.View())
}

// colorizeLogLine applies color to a log line based on its level.
func colorizeLogLine(line string) string {
	lower := strings.ToLower(line)
	switch {
	case strings.Contains(lower, "error"):
		return ErrorStyle.Render(line)
	case strings.Contains(lower, "warn"):
		return StatusWarn.Render(line)
	case strings.Contains(lower, "debug"):
		return DimStyle.Render(line)
	case strings.Contains(lower, "info"):
		return lipgloss.NewStyle().Foreground(ColorLightBlue).Render(line)
	default:
		return line
	}
}

// ---------------------------------------------------------------------------
// HelpView
// ---------------------------------------------------------------------------

// HelpView displays keyboard shortcuts, CLI commands, and about info.
type HelpView struct {
	width  int
	height int
}

// NewHelpView creates a new help view.
func NewHelpView() HelpView {
	return HelpView{}
}

// SetSize updates the help view dimensions.
func (h *HelpView) SetSize(w, ht int) {
	h.width = w
	h.height = ht
}

// Update handles messages for the help view.
func (h *HelpView) Update(msg tea.Msg) tea.Cmd {
	return nil
}

// View renders the help view.
func (h *HelpView) View() string {
	panelWidth := h.width - 8
	if panelWidth < 30 {
		panelWidth = 30
	}

	// --- Keyboard Shortcuts ---
	shortcuts := []struct{ key, desc string }{
		{"Tab / Shift+Tab", "Switch between tabs"},
		{"1 2 3 4", "Jump directly to tab"},
		{"Right / Left", "Next / previous tab"},
		{"j / k", "Scroll down / up (logs)"},
		{"Enter", "Send message (chat)"},
		{"q / Ctrl+C", "Quit (not in chat input)"},
		{"?", "Show this help"},
	}

	var shortcutLines []string
	for _, s := range shortcuts {
		key := HelpKeyStyle.Render(fmt.Sprintf("  %-18s", s.key))
		desc := HelpDescStyle.Render(s.desc)
		shortcutLines = append(shortcutLines, key+desc)
	}
	shortcutsBox := RenderBox("  Keyboard Shortcuts", strings.Join(shortcutLines, "\n"), panelWidth)

	// --- CLI Commands ---
	commands := []struct{ cmd, desc string }{
		{"krill init", "Interactive setup wizard"},
		{"krill dive", "Start Mini Krill"},
		{"krill surface", "Stop Mini Krill"},
		{"krill sonar", "Health check"},
		{"krill tui", "Open this terminal UI"},
		{"krill chat", "Quick chat from CLI"},
		{"krill doctor", "Run diagnostics"},
		{"krill memory list", "List stored memories"},
	}

	var cmdLines []string
	for _, c := range commands {
		cmd := HelpKeyStyle.Render(fmt.Sprintf("  %-20s", c.cmd))
		desc := HelpDescStyle.Render(c.desc)
		cmdLines = append(cmdLines, cmd+desc)
	}
	cmdBox := RenderBox("  CLI Commands", strings.Join(cmdLines, "\n"), panelWidth)

	// --- About ---
	aboutText := lipgloss.JoinVertical(lipgloss.Left,
		AccentStyle.Render("  Mini Krill"),
		"",
		ValueStyle.Render("  A tiny, crustaceous AI agent that lives in your terminal."),
		ValueStyle.Render("  Local-first with Ollama, but can connect to cloud LLMs too."),
		"",
		DimStyle.Render("  Built with: Go, Bubble Tea, Lip Gloss, Ollama"),
		DimStyle.Render("  Inspired by DeepKrill - another crustaceous AI agent"),
		"",
		AccentStyle.Render(fmt.Sprintf("  %s", randomKrillFact())),
	)
	aboutBox := RenderBox("  About", aboutText, panelWidth)

	return lipgloss.JoinVertical(lipgloss.Left,
		"",
		shortcutsBox,
		"",
		cmdBox,
		"",
		aboutBox,
	)
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

// randomKrillFact picks a fact from the global list.
// Uses a simple time-based index so it rotates each tick.
func randomKrillFact() string {
	if len(core.KrillFacts) == 0 {
		return "Krill are amazing."
	}
	idx := int(time.Now().UnixNano()/int64(time.Second)) % len(core.KrillFacts)
	return core.KrillFacts[idx]
}

// formatDuration renders a duration in a human-friendly way.
func formatDuration(d time.Duration) string {
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	return fmt.Sprintf("%dh %dm", h, m)
}

// wordWrap performs simple word wrapping at the given width.
func wordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	words := strings.Fields(text)
	lineLen := 0

	for i, word := range words {
		wLen := len(word)

		if i == 0 {
			result.WriteString(word)
			lineLen = wLen
			continue
		}

		if lineLen+1+wLen > width {
			result.WriteString("\n")
			result.WriteString(word)
			lineLen = wLen
		} else {
			result.WriteString(" ")
			result.WriteString(word)
			lineLen += 1 + wLen
		}
	}

	return result.String()
}
