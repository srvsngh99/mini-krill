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
// Tab indices - use these instead of magic numbers
// ---------------------------------------------------------------------------

const (
	TabDashboard = 0
	TabChat      = 1
	TabSkills    = 2
	TabLogs      = 3
	TabHelp      = 4

	// layoutMargins is the vertical breathing room reserved between layout sections.
	layoutMargins = 3
)

// ---------------------------------------------------------------------------
// App - main Bubble Tea model
// ---------------------------------------------------------------------------

// App is the top-level TUI model that owns all tabs and views.
type App struct {
	tabs      []string
	activeTab int

	dashboard DashboardView
	chat      ChatView
	skills    SkillsView
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
func NewApp(agent core.Agent, brain core.Brain, heartbeat core.Heartbeat, skillReg core.SkillRegistry, mcpReg core.MCPRegistry, version, logFile string) *App {
	return &App{
		tabs:      []string{"Dashboard", "Chat", "Skills", "Logs", "Help"},
		activeTab: 0,
		dashboard: NewDashboardView(version),
		chat:      NewChatView(agent),
		skills:    NewSkillsView(skillReg, mcpReg),
		logs:      NewLogsView(logFile),
		help:      NewHelpView(),
		agent:     agent,
		brain:     brain,
		heartbeat: heartbeat,
		version:   version,
		logFile:   logFile,
	}
}

// SetInitialTab selects which tab is active when the TUI starts.
func (a *App) SetInitialTab(tab int) {
	if tab >= 0 && tab < len(a.tabs) {
		a.activeTab = tab
	}
}

// Init returns the initial command batch - starts the tick loop.
func (a *App) Init() tea.Cmd {
	cmds := []tea.Cmd{tickCmd()}
	if a.activeTab == TabChat {
		cmds = append(cmds, a.chat.input.Focus())
	}
	return tea.Batch(cmds...)
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
		// handleKey already forwards unhandled keys to updateActiveView,
		// so return here to avoid processing the same key twice.
		return a, tea.Batch(cmds...)

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

	// Compact header for all tabs to maximize body space.
	header := RenderCompactHeader(a.version, a.width)
	tabBar := a.renderTabBar()
	body := a.renderBody()
	footer := a.renderFooter()

	bodyHeight := a.bodyHeight()

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
	inChat := a.activeTab == TabChat && a.chat.Focused()

	switch key {
	case "ctrl+c":
		a.quitting = true
		return tea.Quit

	case "esc":
		if inChat {
			a.chat.Blur()
		}
		return nil

	case "q":
		if !inChat {
			a.quitting = true
			return tea.Quit
		}

	case "tab":
		// Tab always switches tabs, even when chat input is focused
		a.activeTab = (a.activeTab + 1) % len(a.tabs)
		a.onTabSwitch()
		return nil

	case "shift+tab":
		// Shift+Tab always switches tabs
		a.activeTab = (a.activeTab - 1 + len(a.tabs)) % len(a.tabs)
		a.onTabSwitch()
		return nil

	case "right":
		if !inChat {
			a.activeTab = (a.activeTab + 1) % len(a.tabs)
			a.onTabSwitch()
			return nil
		}

	case "left":
		if !inChat {
			a.activeTab = (a.activeTab - 1 + len(a.tabs)) % len(a.tabs)
			a.onTabSwitch()
			return nil
		}

	case "1":
		if !inChat {
			a.activeTab = TabDashboard
			a.onTabSwitch()
			return nil
		}
	case "2":
		if !inChat {
			a.activeTab = TabChat
			a.onTabSwitch()
			return nil
		}
	case "3":
		if !inChat {
			a.activeTab = TabSkills
			a.onTabSwitch()
			return nil
		}
	case "4":
		if !inChat {
			a.activeTab = TabLogs
			a.onTabSwitch()
			return nil
		}
	case "5":
		if !inChat {
			a.activeTab = TabHelp
			a.onTabSwitch()
			return nil
		}

	case "?":
		if !inChat {
			a.activeTab = TabHelp
			a.onTabSwitch()
			return nil
		}
	}

	// Forward to active view
	return a.updateActiveView(msg)
}

// onTabSwitch handles focus changes when switching tabs.
// Chat input is NOT auto-focused on switch so that left/right arrows
// remain available for tab navigation. Press Enter to focus the input.
func (a *App) onTabSwitch() {
	a.chat.Blur()

	a.resizeViews()

	switch a.activeTab {
	case TabSkills:
		a.skills.refreshContent()
	case TabLogs:
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
	if a.activeTab == TabLogs {
		a.logs.RefreshLogs()
	}
}

// updateActiveView forwards a message to whichever view is active.
func (a *App) updateActiveView(msg tea.Msg) tea.Cmd {
	switch a.activeTab {
	case TabDashboard:
		return a.dashboard.Update(msg)
	case TabChat:
		return a.chat.Update(msg)
	case TabSkills:
		return a.skills.Update(msg)
	case TabLogs:
		return a.logs.Update(msg)
	case TabHelp:
		return a.help.Update(msg)
	}
	return nil
}

// resizeViews propagates terminal dimensions to all views.
func (a *App) resizeViews() {
	h := a.bodyHeight()
	a.dashboard.SetSize(a.width, h)
	a.chat.SetSize(a.width, h)
	a.skills.SetSize(a.width, h)
	a.logs.SetSize(a.width, h)
	a.help.SetSize(a.width, h)
}

// bodyHeight computes the available vertical space for view content
// by subtracting the actual rendered header, tab bar, and footer heights.
func (a *App) bodyHeight() int {
	header := RenderCompactHeader(a.version, a.width)
	tabBar := a.renderTabBar()
	footer := a.renderFooter()

	headerLines := strings.Count(header, "\n") + 1
	tabLines := strings.Count(tabBar, "\n") + 1
	footerLines := strings.Count(footer, "\n") + 1

	h := a.height - headerLines - tabLines - footerLines - layoutMargins
	if h < 5 {
		h = 5
	}
	return h
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
	case TabDashboard:
		return a.dashboard.View()
	case TabChat:
		return a.chat.View()
	case TabSkills:
		return a.skills.View()
	case TabLogs:
		return a.logs.View()
	case TabHelp:
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

	// Center: keybinding hints
	hints := HelpKeyStyle.Render("Tab") + DimStyle.Render(": switch tabs  ") +
		HelpKeyStyle.Render("?") + DimStyle.Render(": help  ") +
		HelpKeyStyle.Render("q") + DimStyle.Render(": quit")

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
	center := lipgloss.NewStyle().Width(centerWidth).Align(lipgloss.Center).Render(hints)
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
