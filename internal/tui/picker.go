package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// FolderPickerModel is a simple fuzzy folder picker.
type FolderPickerModel struct {
	items     []string
	filtered  []string
	cursor    int
	width     int
	height    int
	cancelled bool
	selected  string
	err       error
	input     textinput.Model
}

// NewFolderPicker creates a picker over the given folder paths.
func NewFolderPicker(items []string) *FolderPickerModel {
	ti := textinput.New()
	ti.Placeholder = "type to filter"
	ti.Focus()
	return &FolderPickerModel{
		items:    items,
		filtered: items,
		input:    ti,
	}
}

// Selected returns the chosen folder, or "" if cancelled.
func (m *FolderPickerModel) Selected() string {
	return m.selected
}

// Cancelled reports whether the user cancelled.
func (m *FolderPickerModel) Cancelled() bool {
	return m.cancelled
}

// Init implements tea.Model.
func (m *FolderPickerModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update implements tea.Model.
func (m *FolderPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEnter:
			if m.cursor < len(m.filtered) {
				m.selected = m.filtered[m.cursor]
				return m, tea.Quit
			}
		case tea.KeyUp:
			if m.cursor > 0 {
				m.cursor--
			}
			return m, nil
		case tea.KeyDown:
			if m.cursor < len(m.filtered)-1 {
				m.cursor++
			}
			return m, nil
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.input.Width = msg.Width - 4
	case error:
		m.err = msg
		return m, tea.Quit
	}

	m.input, cmd = m.input.Update(msg)
	m.updateFilter()
	return m, cmd
}

func (m *FolderPickerModel) updateFilter() {
	q := strings.ToLower(strings.TrimSpace(m.input.Value()))
	if q == "" {
		m.filtered = m.items
	} else {
		// Build a fresh slice so we never write into m.items' backing array
		// (m.filtered may alias m.items, e.g. after an empty query).
		filtered := make([]string, 0, len(m.items))
		for _, item := range m.items {
			if strings.Contains(strings.ToLower(item), q) {
				filtered = append(filtered, item)
			}
		}
		m.filtered = filtered
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
	}
}

// View implements tea.Model.
func (m *FolderPickerModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n", m.err))
	}
	var b string
	b += titleStyle.Render("Select project folder") + "\n\n"
	b += m.input.View() + "\n\n"
	if len(m.filtered) == 0 {
		b += mutedStyle.Render("No matching folders.\n")
	} else {
		pageSize := m.height - 6
		if pageSize < 1 {
			pageSize = len(m.filtered)
		}
		start := 0
		if m.cursor >= pageSize {
			start = m.cursor - pageSize + 1
		}
		end := start + pageSize
		if end > len(m.filtered) {
			end = len(m.filtered)
		}
		for i := start; i < end; i++ {
			prefix := "  "
			if i == m.cursor {
				prefix = cursorStyle.Render("> ")
			}
			b += fmt.Sprintf("%s%s\n", prefix, m.filtered[i])
		}
	}
	b += "\n" + helpStyle.Render("enter to select, esc to cancel, ↑/↓ to move")
	return b
}

var _ tea.Model = &FolderPickerModel{}
