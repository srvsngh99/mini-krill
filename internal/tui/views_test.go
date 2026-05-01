package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/srvsngh99/mini-krill/internal/core"
)

// ---------------------------------------------------------------------------
// Mock MCP Registry for view tests
// ---------------------------------------------------------------------------

type mockMCPRegistry struct {
	servers  []core.MCPServerInfo
	toggled  map[string]bool // tracks SetEnabled calls
	toggleFn func(name string, enabled bool) error
}

func newMockMCPRegistry(servers ...core.MCPServerInfo) *mockMCPRegistry {
	return &mockMCPRegistry{
		servers: servers,
		toggled: make(map[string]bool),
	}
}

func (r *mockMCPRegistry) Register(_ string, _ core.MCPServer) error { return nil }
func (r *mockMCPRegistry) Get(_ string) (core.MCPServer, bool)       { return nil, false }
func (r *mockMCPRegistry) List() []core.MCPServerInfo                { return r.servers }
func (r *mockMCPRegistry) AllTools() []core.MCPTool                  { return nil }
func (r *mockMCPRegistry) Close() error                              { return nil }
func (r *mockMCPRegistry) IsEnabled(_ string) bool                   { return true }
func (r *mockMCPRegistry) SetEnabled(name string, enabled bool) error {
	r.toggled[name] = enabled
	if r.toggleFn != nil {
		return r.toggleFn(name, enabled)
	}
	return nil
}

func TestWordWrap(t *testing.T) {
	tests := []struct {
		name  string
		text  string
		width int
		want  string
	}{
		{
			name:  "empty string",
			text:  "",
			width: 40,
			want:  "",
		},
		{
			name:  "single word within width",
			text:  "hello",
			width: 40,
			want:  "hello",
		},
		{
			name:  "width zero returns original",
			text:  "hello world",
			width: 0,
			want:  "hello world",
		},
		{
			name:  "negative width returns original",
			text:  "hello world",
			width: -1,
			want:  "hello world",
		},
		{
			name:  "short line no wrap needed",
			text:  "hello world",
			width: 40,
			want:  "hello world",
		},
		{
			name:  "wraps at width",
			text:  "one two three four",
			width: 10,
			want:  "one two\n  three\n  four",
		},
		{
			name:  "preserves blank line separators",
			text:  "paragraph one\n\nparagraph two",
			width: 40,
			want:  "paragraph one\n\nparagraph two",
		},
		{
			name:  "preserves list items",
			text:  "- item one\n- item two\n- item three",
			width: 40,
			want:  "- item one\n- item two\n- item three",
		},
		{
			name:  "preserves indentation",
			text:  "  indented line",
			width: 40,
			want:  "  indented line",
		},
		{
			name:  "wraps indented line with continuation",
			text:  "  this is a very long indented line that needs wrapping",
			width: 25,
			want:  "  this is a very long\n    indented line that\n    needs wrapping",
		},
		{
			name:  "multiple paragraphs with lists",
			text:  "Header\n\n- first item\n- second item\n\nFooter",
			width: 40,
			want:  "Header\n\n- first item\n- second item\n\nFooter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := wordWrap(tt.text, tt.width)
			if got != tt.want {
				t.Errorf("wordWrap(%q, %d)\n  got:  %q\n  want: %q", tt.text, tt.width, got, tt.want)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// MCPView tests
// ---------------------------------------------------------------------------

// newReadyMCPView creates an MCPView with dimensions set so it's ready for use.
func newReadyMCPView(reg core.MCPRegistry) MCPView {
	v := NewMCPView(reg)
	v.SetSize(100, 40)
	return v
}

func TestMCPViewCursorClampEmpty(t *testing.T) {
	v := newReadyMCPView(newMockMCPRegistry())

	// Cursor should be 0 on empty list
	if v.cursor != 0 {
		t.Errorf("expected cursor 0 on empty list, got %d", v.cursor)
	}

	// Navigation on empty list should not panic
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	v.Update(tea.KeyMsg{Type: tea.KeyEnter})

	if v.cursor != 0 {
		t.Errorf("expected cursor to stay 0 after nav on empty list, got %d", v.cursor)
	}
}

func TestMCPViewCursorNavigation(t *testing.T) {
	reg := newMockMCPRegistry(
		core.MCPServerInfo{Name: "alpha", Connected: true, ToolCount: 3, Enabled: true},
		core.MCPServerInfo{Name: "beta", Connected: false, ToolCount: 0, Enabled: true},
		core.MCPServerInfo{Name: "gamma", Connected: true, ToolCount: 5, Enabled: false},
	)
	v := newReadyMCPView(reg)

	// Start at 0
	if v.cursor != 0 {
		t.Fatalf("expected initial cursor 0, got %d", v.cursor)
	}

	// j moves down
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if v.cursor != 1 {
		t.Errorf("expected cursor 1 after j, got %d", v.cursor)
	}

	// k moves back up
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if v.cursor != 0 {
		t.Errorf("expected cursor 0 after k, got %d", v.cursor)
	}

	// k at top stays at 0
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	if v.cursor != 0 {
		t.Errorf("expected cursor to stay 0 at top, got %d", v.cursor)
	}

	// G jumps to bottom
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'G'}})
	if v.cursor != 2 {
		t.Errorf("expected cursor 2 after G, got %d", v.cursor)
	}

	// j at bottom stays at bottom
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	if v.cursor != 2 {
		t.Errorf("expected cursor to stay 2 at bottom, got %d", v.cursor)
	}

	// g jumps to top
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if v.cursor != 0 {
		t.Errorf("expected cursor 0 after g, got %d", v.cursor)
	}
}

func TestMCPViewToggleDispatchesSetEnabled(t *testing.T) {
	reg := newMockMCPRegistry(
		core.MCPServerInfo{Name: "server-a", Connected: true, ToolCount: 2, Enabled: true},
		core.MCPServerInfo{Name: "server-b", Connected: false, ToolCount: 0, Enabled: false},
	)
	v := newReadyMCPView(reg)

	// Toggle first item (enabled -> disabled)
	v.Update(tea.KeyMsg{Type: tea.KeyEnter})

	enabled, ok := reg.toggled["server-a"]
	if !ok {
		t.Fatal("expected SetEnabled to be called for server-a")
	}
	if enabled != false {
		t.Errorf("expected server-a toggled to false, got %v", enabled)
	}

	// Move to second item and toggle (disabled -> enabled)
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	v.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})

	enabled, ok = reg.toggled["server-b"]
	if !ok {
		t.Fatal("expected SetEnabled to be called for server-b")
	}
	if enabled != true {
		t.Errorf("expected server-b toggled to true, got %v", enabled)
	}
}

func TestMCPViewNilRegistry(t *testing.T) {
	v := newReadyMCPView(nil)

	// Should not panic with nil registry
	if len(v.items) != 0 {
		t.Errorf("expected 0 items with nil registry, got %d", len(v.items))
	}

	// Toggle on nil registry should not panic
	v.Update(tea.KeyMsg{Type: tea.KeyEnter})
}
