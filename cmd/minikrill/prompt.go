package main

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// selectItem represents a single option in an interactive selection list.
type selectItem struct {
	label string
	desc  string
}

// selectModel is a Bubble Tea model for arrow-key navigable selection.
type selectModel struct {
	items    []selectItem
	cursor   int
	chosen   int
	quitting bool
}

func (m selectModel) Init() tea.Cmd { return nil }

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.items)-1 {
				m.cursor++
			}
		case "enter":
			m.chosen = m.cursor
			m.quitting = true
			return m, tea.Quit
		case "ctrl+c":
			m.chosen = -1
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m selectModel) View() string {
	// After selection, show only the chosen item as confirmation.
	if m.quitting {
		if m.chosen >= 0 && m.chosen < len(m.items) {
			return fmt.Sprintf("    %s▸ %s%s\n", cBCyan, m.items[m.chosen].label, cReset)
		}
		return ""
	}

	var b strings.Builder
	for i, item := range m.items {
		if i == m.cursor {
			b.WriteString(fmt.Sprintf("    %s▸ %-14s %s%s\n",
				cBCyan, item.label, item.desc, cReset))
		} else {
			b.WriteString(fmt.Sprintf("    %s  %-14s %s%s\n",
				cDim, item.label, item.desc, cReset))
		}
	}
	b.WriteString("\n    " + cDim + "↑/↓ navigate • enter select" + cReset)
	return b.String()
}

// promptSelect shows an interactive arrow-key selection list.
// Returns the chosen index, or -1 if cancelled with Ctrl+C.
func promptSelect(items []selectItem) (int, error) {
	m := selectModel{items: items, chosen: -1}
	p := tea.NewProgram(m)
	result, err := p.Run()
	if err != nil {
		return -1, err
	}
	return result.(selectModel).chosen, nil
}
