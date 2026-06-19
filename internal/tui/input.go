package tui

import (
	"fmt"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
)

// InputModel is a single-line text input prompt.
type InputModel struct {
	prompt    string
	cancelled bool
	submitted bool
	err       error
	input     textinput.Model
}

// NewInput creates an input model.
func NewInput(prompt string) *InputModel {
	ti := textinput.New()
	ti.Placeholder = "type here"
	ti.Focus()
	return &InputModel{
		prompt: prompt,
		input:  ti,
	}
}

// Value returns the entered text.
func (m *InputModel) Value() string {
	return m.input.Value()
}

// Submitted reports whether the user pressed Enter.
func (m *InputModel) Submitted() bool {
	return m.submitted
}

// Cancelled reports whether the user cancelled.
func (m *InputModel) Cancelled() bool {
	return m.cancelled
}

// Init implements tea.Model.
func (m *InputModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update implements tea.Model.
func (m *InputModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			m.cancelled = true
			return m, tea.Quit
		case tea.KeyEnter:
			m.submitted = true
			return m, tea.Quit
		}
	case tea.WindowSizeMsg:
		m.input.Width = msg.Width - 4
	case error:
		m.err = msg
		return m, tea.Quit
	}

	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

// View implements tea.Model.
func (m *InputModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n", m.err))
	}
	return fmt.Sprintf("%s\n\n%s", titleStyle.Render(m.prompt), m.input.View())
}

var _ tea.Model = &InputModel{}
