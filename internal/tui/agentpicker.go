package tui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
)

// AgentItem is one row in the agent picker. Installed rows are selectable;
// uninstalled rows render dimmed and are skipped by navigation and Enter. The
// "none" row sets None and is always selectable.
type AgentItem struct {
	Name      string
	Command   string
	Installed bool
	None      bool
}

// AgentPickerModel is a small cursor list over an agent catalog.
type AgentPickerModel struct {
	items     []AgentItem
	cursor    int
	height    int
	cancelled bool
	chose     bool
	selected  AgentItem
}

// NewAgentPicker creates a picker. The cursor starts on the row whose Name
// equals defaultName when that row is selectable, otherwise on the first
// selectable row.
func NewAgentPicker(items []AgentItem, defaultName string) *AgentPickerModel {
	m := &AgentPickerModel{items: items}
	m.cursor = m.firstSelectable()
	for i, it := range items {
		if it.Name == defaultName && it.Installed {
			m.cursor = i
			break
		}
	}
	return m
}

func (m *AgentPickerModel) firstSelectable() int {
	for i, it := range m.items {
		if it.Installed {
			return i
		}
	}
	return 0
}

// Chosen reports the selected item once the user pressed Enter on a selectable
// row. ok is false while the user is still picking, after cancel, or never.
func (m *AgentPickerModel) Chosen() (AgentItem, bool) { return m.selected, m.chose }

// Cancelled reports whether the user dismissed the picker (Esc/Ctrl+C).
func (m *AgentPickerModel) Cancelled() bool { return m.cancelled }

// Init implements tea.Model.
func (m *AgentPickerModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *AgentPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEnter:
			if m.cursor >= 0 && m.cursor < len(m.items) && m.items[m.cursor].Installed {
				m.selected = m.items[m.cursor]
				m.chose = true
				return m, tea.Quit
			}
		case tea.KeyUp:
			m.move(-1)
		case tea.KeyDown:
			m.move(1)
		}
	case tea.WindowSizeMsg:
		m.height = msg.Height
	case error:
		m.cancelled = true
		return m, tea.Quit
	}
	return m, nil
}

// move steps the cursor by dir, skipping uninstalled rows, and stops at the ends.
func (m *AgentPickerModel) move(dir int) {
	i := m.cursor
	for {
		i += dir
		if i < 0 || i >= len(m.items) {
			return // no selectable row in that direction; keep cursor put
		}
		if m.items[i].Installed {
			m.cursor = i
			return
		}
	}
}

// View implements tea.Model.
func (m *AgentPickerModel) View() string {
	b := titleStyle.Render("Pick an agent") + "\n\n"
	for i, it := range m.items {
		prefix := "  "
		if i == m.cursor {
			prefix = cursorStyle.Render("> ")
		}
		switch {
		case it.None:
			b += fmt.Sprintf("%s%s\n", prefix, mutedStyle.Render("— none (just a shell) —"))
		case it.Installed:
			b += fmt.Sprintf("%s%s\n", prefix, it.Name)
		default:
			b += fmt.Sprintf("%s%s\n", prefix, mutedStyle.Render(it.Name+"  (install to use)"))
		}
	}
	b += "\n" + helpStyle.Render("enter to select, esc to cancel, ↑/↓ to move")
	return b
}

var _ tea.Model = &AgentPickerModel{}
