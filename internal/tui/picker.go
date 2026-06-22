package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// FolderPickerModel is a fuzzy folder picker that can also create a new folder:
// when the typed text doesn't match an existing entry, the list offers a synthetic
// "create new folder" row, so `eme new` is not stuck to folders already on disk.
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
	// home anchors a relative or ~-prefixed "create new" path. Empty when the home
	// dir is unknown, in which case only absolute new paths can be created.
	home string
	// createPath is the resolved absolute path the synthetic "create new folder" row
	// would make, or "" when no create row is offered for the current query.
	createPath string
}

// NewFolderPicker creates a picker over the given folder paths.
func NewFolderPicker(items []string) *FolderPickerModel {
	ti := textinput.New()
	ti.Placeholder = "type to filter, or a new folder path to create"
	ti.Focus()
	home, _ := os.UserHomeDir()
	return &FolderPickerModel{
		items:    items,
		filtered: items,
		input:    ti,
		home:     home,
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
			if m.createPath != "" && m.cursor == len(m.filtered) {
				m.selected = m.createPath // the synthetic "create new folder" row
				return m, tea.Quit
			}
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
			if m.cursor < m.rowCount()-1 {
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
	m.createPath = m.computeCreatePath()
	if m.cursor >= m.rowCount() {
		m.cursor = 0
	}
}

// rowCount is the number of selectable rows: the filtered folders plus the
// synthetic "create new folder" row when one is offered.
func (m *FolderPickerModel) rowCount() int {
	n := len(m.filtered)
	if m.createPath != "" {
		n++
	}
	return n
}

// computeCreatePath resolves the typed query to the absolute folder path a "create
// new" row would make, or "" when no create row should be shown. The row is offered
// whenever the query is non-empty and resolves to a folder that is not already an
// exact item in the list — always available (at the bottom of the list) so the user
// is never left at a dead end, even when the typed name happens to be a substring of
// an existing folder.
func (m *FolderPickerModel) computeCreatePath() string {
	q := strings.TrimSpace(m.input.Value())
	if q == "" {
		return ""
	}
	cand := m.resolveCreatePath(q)
	if cand == "" {
		return ""
	}
	for _, it := range m.items {
		if it == cand {
			return "" // already directly selectable in the list
		}
	}
	return cand
}

// resolveCreatePath turns a typed query into an absolute path: a leading ~ expands to
// home, and a relative path is anchored at home. Returns "" when a relative path
// cannot be anchored because the home dir is unknown.
func (m *FolderPickerModel) resolveCreatePath(q string) string {
	switch {
	case q == "~":
		q = m.home
	case strings.HasPrefix(q, "~/"):
		q = filepath.Join(m.home, q[2:])
	}
	if !filepath.IsAbs(q) {
		if m.home == "" {
			return ""
		}
		q = filepath.Join(m.home, q)
	}
	return filepath.Clean(q)
}

// View implements tea.Model.
func (m *FolderPickerModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n", m.err))
	}
	var b string
	b += titleStyle.Render("Select project folder") + "\n\n"
	b += m.input.View() + "\n\n"
	n := m.rowCount()
	if n == 0 {
		b += mutedStyle.Render("No matching folders. Type a new path to create one.\n")
	} else {
		pageSize := m.height - 6
		if pageSize < 1 {
			pageSize = n
		}
		start := 0
		if m.cursor >= pageSize {
			start = m.cursor - pageSize + 1
		}
		end := start + pageSize
		if end > n {
			end = n
		}
		for i := start; i < end; i++ {
			b += m.renderRow(i)
		}
	}
	b += "\n" + helpStyle.Render("enter to select/create · esc to cancel · ↑/↓ to move")
	return b
}

// renderRow renders the i-th selectable row: a filtered folder, or the synthetic
// "create new folder" row in the last slot when one is offered.
func (m *FolderPickerModel) renderRow(i int) string {
	prefix := "  "
	if i == m.cursor {
		prefix = cursorStyle.Render("> ")
	}
	if m.createPath != "" && i == len(m.filtered) {
		return fmt.Sprintf("%s%s%s\n", prefix, mutedStyle.Render("+ create new folder  "), textStyle.Render(m.createPath))
	}
	return fmt.Sprintf("%s%s\n", prefix, m.filtered[i])
}

var _ tea.Model = &FolderPickerModel{}
