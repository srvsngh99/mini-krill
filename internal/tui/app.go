package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/srvsngh99/mini-krill/internal/core"
	log "github.com/srvsngh99/mini-krill/internal/log"
)

// ---------------------------------------------------------------------------
// Custom messages
// ---------------------------------------------------------------------------

// tickMsg fires every few seconds to refresh heartbeat and rotate facts.
type tickMsg time.Time

// chatResponseMsg carries the agent's reply back to the chat view.
type chatResponseMsg struct {
	response string
	err      error
}

// ---------------------------------------------------------------------------
// App - main Bubble Tea model
// ---------------------------------------------------------------------------

// App is the top-level TUI model that owns all tabs and views.
type App struct {
	tabs      []string
	activeTab int

	dashboard DashboardView
	chat      ChatView
	logs      LogsView
	help      HelpView

	agent     core.Agent
	brain     core.Brain
	heartbeat core.Heartbeat
	version   string
	logFile   string

	width    int
	height   int
	quitting bool
}

// NewApp creates a fully initialized App ready to run.
func NewApp(agent core.Agent, brain core.Brain, heartbeat core.Heartbeat, version, logFile string) *App {
	return &App{
		tabs:      []string{"Dashboard", "Chat", "Logs", "Help"},
		activeTab: 0,
		dashboard: NewDashboardView(version),
		chat:      NewChatView(agent),
		logs:      NewLogsView(logFile),
		help:      NewHelpView(),
		agent:     agent,
		brain:     brain,
		heartbeat: heartbeat,
		version:   version,
		logFile:   logFile,
	}
}

// Init returns the initial command batch - starts the tick loop.
func (a *App) Init() tea.Cmd {
	return tea.Batch(
		tickCmd(),
		a.chat.input.Focus(),
	)
}

// Update processes all incoming messages and dispatches to the active view.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmds []tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.resizeViews()
		return a, nil

	case tea.KeyMsg:
		cmd := a.handleKey(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		if a.quitting {
			return a, tea.Quit
		}

	case tickMsg:
		a.onTick()
		cmds = append(cmds, tickCmd())

	case chatResponseMsg:
		cmd := a.chat.Update(msg)
		if cmd != nil {
			cmds = append(cmds, cmd)
		}
		return a, tea.Batch(cmds...)
	}

	// Forward remaining messages to active view
	cmd := a.updateActiveView(msg)
	if cmd != nil {
		cmds = append(cmds, cmd)
	}

	return a, tea.Batch(cmds...)
}

// View renders the complete TUI layout.
func (a *App) View() string {
	if a.quitting {
		return "\n  " + AccentStyle.Render("Krill is surfacing... goodbye!") + "\n\n"
	}

	if a.width == 0 {
		return "\n  Waiting for terminal..."
	}

	// Build layout: header + tabs + body + footer
	header := RenderHeader(a.version)
	tabBar := a.renderTabBar()
	body := a.renderBody()
	footer := a.renderFooter()

	// Calculate body height: total - header - tabs - footer - margins
	headerLines := strings.Count(header, "\n") + 1
	tabLines := strings.Count(tabBar, "\n") + 1
	footerLines := strings.Count(footer, "\n") + 1
	margins := 3 // breathing room

	bodyHeight := a.height - headerLines - tabLines - footerLines - margins
	if bodyHeight < 5 {
		bodyHeight = 5
	}

	// Constrain body to exact height
	bodyStyled := lipgloss.NewStyle().
		Height(bodyHeight).
		MaxHeight(bodyHeight).
		Width(a.width).
		Render(body)

	return lipgloss.JoinVertical(
		lipgloss.Left,
		header,
		tabBar,
		bodyStyled,
		footer,
	)
}

// Run starts the Bubble Tea program with alt screen.
func (a *App) Run() error {
	log.Info("starting TUI", "version", a.version)
	p := tea.NewProgram(a, tea.WithAltScreen(), tea.WithMouseCellMotion())
	_, err := p.Run()
	return err
}

// ---------------------------------------------------------------------------
// Internal methods
// ---------------------------------------------------------------------------

// handleKey processes keyboard input, handling global shortcuts first.
func (a *App) handleKey(msg tea.KeyMsg) tea.Cmd {
	key := msg.String()

	// In chat tab with focused input, only intercept global quit keys
	inChat := a.activeTab == 1 && a.chat.Focused()

	switch key {
	case "ctrl+c":
		a.quitting = true
		return tea.Quit

	case "q":
		if !inChat {
			a.quitting = true
			return tea.Quit
		}

	case "tab", "right":
		if !inChat {
			a.activeTab = (a.activeTab + 1) % len(a.tabs)
			a.onTabSwitch()
			return nil
		}

	case "shift+tab", "left":
		if !inChat {
			a.activeTab = (a.activeTab - 1 + len(a.tabs)) % len(a.tabs)
			a.onTabSwitch()
			return nil
		}

	case "1":
		if !inChat {
			a.activeTab = 0
			a.onTabSwitch()
			return nil
		}
	case "2":
		if !inChat {
			a.activeTab = 1
			a.onTabSwitch()
			return nil
		}
	case "3":
		if !inChat {
			a.activeTab = 2
			a.onTabSwitch()
			return nil
		}
	case "4":
		if !inChat {
			a.activeTab = 3
			a.onTabSwitch()
			return nil
		}

	case "?":
		if !inChat {
			a.activeTab = 3 // jump to help
			a.onTabSwitch()
			return nil
		}
	}

	// Forward to active view
	return a.updateActiveView(msg)
}

// onTabSwitch handles focus changes when switching tabs.
func (a *App) onTabSwitch() {
	if a.activeTab == 1 {
		a.chat.Focus()
	} else {
		a.chat.Blur()
	}

	// Refresh logs when switching to logs tab
	if a.activeTab == 2 {
		a.logs.RefreshLogs()
	}
}

// onTick refreshes heartbeat status and rotates krill facts.
func (a *App) onTick() {
	// Update heartbeat status
	if a.heartbeat != nil {
		a.dashboard.status = a.heartbeat.Status()
	}

	// Rotate krill fact
	a.dashboard.fact = randomKrillFact()

	// Refresh logs if on logs tab
	if a.activeTab == 2 {
		a.logs.RefreshLogs()
	}
}

// updateActiveView forwards a message to whichever view is active.
func (a *App) updateActiveView(msg tea.Msg) tea.Cmd {
	switch a.activeTab {
	case 0:
		return a.dashboard.Update(msg)
	case 1:
		return a.chat.Update(msg)
	case 2:
		return a.logs.Update(msg)
	case 3:
		return a.help.Update(msg)
	}
	return nil
}

// resizeViews propagates terminal dimensions to all views.
func (a *App) resizeViews() {
	// Reserve space for header, tabs, footer
	bodyHeight := a.height - 10
	if bodyHeight < 5 {
		bodyHeight = 5
	}

	a.dashboard.SetSize(a.width, bodyHeight)
	a.chat.SetSize(a.width, bodyHeight)
	a.logs.SetSize(a.width, bodyHeight)
	a.help.SetSize(a.width, bodyHeight)
}

// renderTabBar builds the horizontal tab bar with active highlighting.
func (a *App) renderTabBar() string {
	var tabs []string

	for i, name := range a.tabs {
		// Add number prefix for quick-jump hint
		label := fmt.Sprintf(" %d %s ", i+1, name)

		if i == a.activeTab {
			tabs = append(tabs, ActiveTabStyle.Render(label))
		} else {
			tabs = append(tabs, TabStyle.Render(label))
		}
	}

	row := lipgloss.JoinHorizontal(lipgloss.Bottom, tabs...)

	// Fill the rest of the tab bar with a border line
	gap := a.width - lipgloss.Width(row)
	if gap > 0 {
		filler := lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(ColorDimBlue).
			Render(strings.Repeat(" ", gap))
		row = lipgloss.JoinHorizontal(lipgloss.Bottom, row, filler)
	}

	return row
}

// renderBody returns the active view's rendered content.
func (a *App) renderBody() string {
	switch a.activeTab {
	case 0:
		return a.dashboard.View()
	case 1:
		return a.chat.View()
	case 2:
		return a.logs.View()
	case 3:
		return a.help.View()
	default:
		return ""
	}
}

// renderFooter builds the bottom status bar.
func (a *App) renderFooter() string {
	// Left: status + activity
	var statusText string
	if a.chat.waiting {
		statusText = AccentStyle.Render("  Diving deep...")
	} else {
		statusText = DimStyle.Render("  Swimming...")
	}

	// Center: current krill fact (truncated if needed)
	fact := a.dashboard.fact
	maxFactWidth := a.width - 40
	if maxFactWidth < 10 {
		maxFactWidth = 10
	}
	if len(fact) > maxFactWidth {
		fact = fact[:maxFactWidth-3] + "..."
	}
	factText := DimStyle.Render(fact)

	// Right: connection status
	var connStatus string
	if a.heartbeat != nil {
		status := a.heartbeat.Status()
		if status.Alive {
			connStatus = RenderStatus("ok")
		} else {
			connStatus = RenderStatus("down")
		}
	} else {
		connStatus = RenderStatus("idle")
	}

	// Compose footer
	leftWidth := 20
	rightWidth := 10
	centerWidth := a.width - leftWidth - rightWidth - 4

	left := lipgloss.NewStyle().Width(leftWidth).Render(statusText)
	center := lipgloss.NewStyle().Width(centerWidth).Align(lipgloss.Center).Render(factText)
	right := lipgloss.NewStyle().Width(rightWidth).Align(lipgloss.Right).Render(connStatus)

	content := lipgloss.JoinHorizontal(lipgloss.Center, left, center, right)
	return FooterStyle.Width(a.width).Render(content)
}

// ---------------------------------------------------------------------------
// Commands
// ---------------------------------------------------------------------------

// tickCmd returns a command that fires a tickMsg after the interval.
func tickCmd() tea.Cmd {
	return tea.Tick(5*time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}
