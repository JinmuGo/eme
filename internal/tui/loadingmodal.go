package tui

import (
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// LoadingModal is a minimal overlay shown while a slow step (e.g. a network fetch)
// runs. It owns no work itself — the dashboard fires the async tea.Cmd and swaps
// this modal out when the result arrives. Esc/Ctrl-C cancels.
type LoadingModal struct {
	message   string
	cancelled bool
	width     int
	height    int
}

// NewLoadingModal creates a loading overlay showing message.
func NewLoadingModal(message string) *LoadingModal {
	return &LoadingModal{message: message}
}

// Cancelled reports whether the user dismissed the modal.
func (m *LoadingModal) Cancelled() bool { return m.cancelled }

// Init implements tea.Model.
func (m *LoadingModal) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *LoadingModal) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	}
	return m, nil
}

// View implements tea.Model.
func (m *LoadingModal) View() string {
	box := m.Box()
	if m.width <= 0 || m.height <= 0 {
		return box
	}
	return lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, box)
}

// Box renders the dialog without centering, for the dashboard overlay.
func (m *LoadingModal) Box() string {
	return dialogStyle.Render(titleStyle.Render(m.message) + "\n\n" + helpStyle.Render("esc to cancel"))
}

var _ overlayModal = &LoadingModal{}
