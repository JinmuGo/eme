package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// RepoItem is one selectable repository in the clone picker. It is tui-local so
// the package stays decoupled from the gh data source.
type RepoItem struct {
	NameWithOwner string
	Description   string
	Private       bool
}

// RepoPickerModel is a fuzzy picker over a list of GitHub repositories, used by
// `eme clone` with no argument.
type RepoPickerModel struct {
	items     []RepoItem
	filtered  []RepoItem
	cursor    int
	width     int
	height    int
	cancelled bool
	selected  RepoItem
	input     textinput.Model
}

// NewRepoPicker creates a picker over the given repositories.
func NewRepoPicker(items []RepoItem) *RepoPickerModel {
	ti := textinput.New()
	ti.Placeholder = "type to filter your GitHub repos"
	ti.Focus()
	return &RepoPickerModel{items: items, filtered: items, input: ti}
}

// Selected returns the chosen repository (zero value if cancelled).
func (m *RepoPickerModel) Selected() RepoItem { return m.selected }

// Cancelled reports whether the user dismissed the picker without choosing.
func (m *RepoPickerModel) Cancelled() bool { return m.cancelled }

// Init implements tea.Model.
func (m *RepoPickerModel) Init() tea.Cmd { return textinput.Blink }

// Update implements tea.Model.
func (m *RepoPickerModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
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
	}
	// A non-fatal error message (e.g. a paste error) falls through to textinput,
	// which handles it without quitting — so it never collapses into an empty
	// selection that the caller would misread as a chosen repo.
	m.input, cmd = m.input.Update(msg)
	m.updateFilter()
	return m, cmd
}

func (m *RepoPickerModel) updateFilter() {
	q := strings.ToLower(strings.TrimSpace(m.input.Value()))
	if q == "" {
		m.filtered = m.items
	} else {
		filtered := make([]RepoItem, 0, len(m.items))
		for _, it := range m.items {
			if strings.Contains(strings.ToLower(it.NameWithOwner), q) {
				filtered = append(filtered, it)
			}
		}
		m.filtered = filtered
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = 0
	}
}

// View implements tea.Model.
func (m *RepoPickerModel) View() string {
	box := dialogStyle.Render(m.content())
	if m.width <= 0 || m.height <= 0 {
		return box
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

func (m *RepoPickerModel) content() string {
	b := titleStyle.Render("Clone a GitHub repo") + "\n\n"
	b += m.input.View() + "\n\n"
	if len(m.filtered) == 0 {
		b += mutedStyle.Render("No matching repositories.") + "\n"
	} else {
		const maxRows = 12
		start := 0
		if m.cursor >= maxRows {
			start = m.cursor - maxRows + 1
		}
		end := start + maxRows
		if end > len(m.filtered) {
			end = len(m.filtered)
		}
		for i := start; i < end; i++ {
			b += m.renderRow(i)
		}
	}
	b += "\n" + helpStyle.Render("enter to clone · esc to cancel · ↑/↓ to move")
	return b
}

func (m *RepoPickerModel) renderRow(i int) string {
	prefix := "  "
	if i == m.cursor {
		prefix = cursorStyle.Render("> ")
	}
	it := m.filtered[i]
	lock := ""
	if it.Private {
		lock = mutedStyle.Render(" [private]")
	}
	row := fmt.Sprintf("%s%s%s", prefix, textStyle.Render(it.NameWithOwner), lock)
	if it.Description != "" {
		row += "  " + mutedStyle.Render(it.Description)
	}
	return row + "\n"
}

var _ tea.Model = &RepoPickerModel{}
