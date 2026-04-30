package tui

import (
	"testing"
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
