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
