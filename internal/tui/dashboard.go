// Package tui implements the eme terminal user interface.
package tui

import (
	"fmt"
	"os"
	"syscall"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jinmu/eme/internal/state"
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7D56F4"))
	cursorStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#04B575"))
	mutedStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	windowStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#CCCCCC"))
	errorStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#FF5555"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
)

// DashboardModel is the main dashboard.
type DashboardModel struct {
	sessions []state.Session
	cursor   int
	width    int
	height   int
	err      error
	showHelp bool
}

// NewDashboard creates a dashboard model.
func NewDashboard(sessions []state.Session) *DashboardModel {
	return &DashboardModel{sessions: sessions}
}

// Init implements tea.Model.
func (m *DashboardModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c", "esc":
			return m, tea.Quit
		case "?":
			m.showHelp = !m.showHelp
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.sessions)-1 {
				m.cursor++
			}
		case "enter", "o":
			if m.cursor < len(m.sessions) {
				m.exec("switch", m.sessions[m.cursor].ID)
			}
		case "n":
			m.exec("new")
		case "c":
			if m.cursor < len(m.sessions) {
				m.exec("new", "--worktree", m.sessions[m.cursor].ID)
			}
		case "d":
			if m.cursor < len(m.sessions) {
				m.exec("kill", m.sessions[m.cursor].ID)
			}
		case "a":
			if m.cursor < len(m.sessions) {
				m.exec("agent", m.sessions[m.cursor].ID)
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case error:
		m.err = msg
		return m, tea.Quit
	}
	return m, nil
}

// View implements tea.Model.
func (m *DashboardModel) View() string {
	if m.err != nil {
		return errorStyle.Render(fmt.Sprintf("Error: %v\n", m.err))
	}
	var b string
	b += titleStyle.Render("eme dashboard") + "\n\n"
	if len(m.sessions) == 0 {
		b += mutedStyle.Render("No sessions. Press 'n' to create one.\n")
	} else {
		for i, sess := range m.sessions {
			prefix := "  "
			if i == m.cursor {
				prefix = cursorStyle.Render("> ")
			}
			b += fmt.Sprintf("%s%s  %s\n", prefix, sess.DisplayName, mutedStyle.Render(sess.Root))
			for _, w := range sess.Worktrees {
				status := ""
				if w.AgentPID > 0 {
					status = mutedStyle.Render(" [agent]")
				}
				b += fmt.Sprintf("    %s%s  %s%s\n", windowStyle.Render("-"), w.Name, mutedStyle.Render(w.Branch), status)
			}
		}
	}
	b += "\n"
	if m.showHelp {
		b += helpStyle.Render("n: new  c: create worktree  enter/o: open  d: kill  a: agent  q: quit  ?: help") + "\n"
	} else {
		b += helpStyle.Render("?: help") + "\n"
	}
	return b
}

func (m *DashboardModel) exec(args ...string) {
	binary, err := os.Executable()
	if err != nil {
		m.err = fmt.Errorf("locate eme binary: %w", err)
		return
	}
	argv := append([]string{"eme"}, args...)
	err = syscall.Exec(binary, argv, os.Environ())
	// If exec succeeds, we never return. If it fails:
	m.err = fmt.Errorf("exec eme %v: %w", args, err)
}
