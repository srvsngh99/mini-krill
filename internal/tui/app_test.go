package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestSetInitialTab(t *testing.T) {
	app := NewApp(nil, nil, nil, "test", "")

	// Default tab is dashboard
	if app.activeTab != TabDashboard {
		t.Errorf("expected default tab %d, got %d", TabDashboard, app.activeTab)
	}

	// Set to chat tab
	app.SetInitialTab(TabChat)
	if app.activeTab != TabChat {
		t.Errorf("expected tab %d, got %d", TabChat, app.activeTab)
	}

	// Out-of-bounds positive: should not change
	app.SetInitialTab(99)
	if app.activeTab != TabChat {
		t.Errorf("expected tab to stay %d after out-of-bounds, got %d", TabChat, app.activeTab)
	}

	// Negative: should not change
	app.SetInitialTab(-1)
	if app.activeTab != TabChat {
		t.Errorf("expected tab to stay %d after negative, got %d", TabChat, app.activeTab)
	}

	// Set to each valid tab
	for _, tab := range []int{TabDashboard, TabChat, TabLogs, TabHelp} {
		app.SetInitialTab(tab)
		if app.activeTab != tab {
			t.Errorf("expected tab %d, got %d", tab, app.activeTab)
		}
	}
}

// newSizedApp creates an App with dimensions set so handleKey works correctly.
func newSizedApp() *App {
	app := NewApp(nil, nil, nil, "test", "")
	app.width = 100
	app.height = 40
	return app
}

func TestTabAlwaysSwitchesTabs(t *testing.T) {
	app := newSizedApp()
	app.activeTab = TabChat
	app.chat.Focus()

	if !app.chat.Focused() {
		t.Fatal("expected chat input to be focused")
	}

	// Tab should switch away from chat even when input is focused
	app.handleKey(tea.KeyMsg{Type: tea.KeyTab})
	if app.activeTab != TabLogs {
		t.Errorf("expected Tab to switch to TabLogs (%d), got %d", TabLogs, app.activeTab)
	}
}

func TestShiftTabAlwaysSwitchesTabs(t *testing.T) {
	app := newSizedApp()
	app.activeTab = TabChat
	app.chat.Focus()

	app.handleKey(tea.KeyMsg{Type: tea.KeyShiftTab})
	if app.activeTab != TabDashboard {
		t.Errorf("expected Shift+Tab to switch to TabDashboard (%d), got %d", TabDashboard, app.activeTab)
	}
}

func TestEscBlursChatInput(t *testing.T) {
	app := newSizedApp()
	app.activeTab = TabChat
	app.chat.Focus()

	if !app.chat.Focused() {
		t.Fatal("expected chat input to be focused before Esc")
	}

	app.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
	if app.chat.Focused() {
		t.Error("expected chat input to be blurred after Esc")
	}
}

// TestUpdateKeyMsgNotDoubleDispatched verifies that App.Update returns after
// handleKey without forwarding the same KeyMsg to updateActiveView a second
// time. This is a regression test for the double-character-input bug.
func TestUpdateKeyMsgNotDoubleDispatched(t *testing.T) {
	app := newSizedApp()
	app.activeTab = TabChat
	app.chat.Focus()
	// Resize so the chat view is ready
	app.Update(tea.WindowSizeMsg{Width: 100, Height: 40})

	// Type a character via Update (not handleKey) to exercise the full path.
	app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})

	got := app.chat.input.Value()
	if got != "a" {
		t.Errorf("expected input value %q after one keypress, got %q (double dispatch?)", "a", got)
	}
}

func TestLeftRightAlwaysSwitchTabs(t *testing.T) {
	app := newSizedApp()
	app.activeTab = TabChat
	app.chat.Focus()

	if !app.chat.Focused() {
		t.Fatal("expected chat input to be focused")
	}

	// Right should switch from Chat to Logs even when chat is focused
	app.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if app.activeTab != TabLogs {
		t.Errorf("expected Right to switch to TabLogs (%d), got %d", TabLogs, app.activeTab)
	}

	// Go back to Chat and re-focus
	app.activeTab = TabChat
	app.chat.Focus()

	// Left should switch from Chat to Dashboard
	app.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if app.activeTab != TabDashboard {
		t.Errorf("expected Left to switch to TabDashboard (%d), got %d", TabDashboard, app.activeTab)
	}
}

func TestLeftRightWrapAround(t *testing.T) {
	app := newSizedApp()

	// From Dashboard, Left should wrap to Help
	app.activeTab = TabDashboard
	app.handleKey(tea.KeyMsg{Type: tea.KeyLeft})
	if app.activeTab != TabHelp {
		t.Errorf("expected Left from Dashboard to wrap to TabHelp (%d), got %d", TabHelp, app.activeTab)
	}

	// From Help, Right should wrap to Dashboard
	app.activeTab = TabHelp
	app.handleKey(tea.KeyMsg{Type: tea.KeyRight})
	if app.activeTab != TabDashboard {
		t.Errorf("expected Right from Help to wrap to TabDashboard (%d), got %d", TabDashboard, app.activeTab)
	}
}

func TestEscDoesNotForwardWhenNotInChat(t *testing.T) {
	app := newSizedApp()
	app.activeTab = TabDashboard
	startTab := app.activeTab

	cmd := app.handleKey(tea.KeyMsg{Type: tea.KeyEscape})
	if cmd != nil {
		t.Error("expected Esc on Dashboard to return nil cmd")
	}
	if app.activeTab != startTab {
		t.Errorf("expected tab to stay %d, got %d", startTab, app.activeTab)
	}
}
