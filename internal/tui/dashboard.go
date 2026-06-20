// Package tui implements the eme terminal user interface.
package tui

import (
	"fmt"
	"os"
	"os/exec"
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
	notice   string
	// pendingKill is the session ID awaiting a kill confirmation, or "" when no
	// confirmation is in progress.
	pendingKill string
	showHelp    bool
	// reload re-reads the (reconciled) session list after a child action
	// returns. It may be nil (e.g. in tests), in which case the list is not
	// refreshed.
	reload func() ([]state.Session, error)
}

// NewDashboard creates a dashboard model. reload is called after each child
// action (create/kill/agent) completes to refresh the session list.
func NewDashboard(sessions []state.Session, reload func() ([]state.Session, error)) *DashboardModel {
	return &DashboardModel{sessions: sessions, reload: reload}
}

// actionFinishedMsg is delivered after a child `eme` process — launched for a
// create/kill/agent action — exits and the dashboard regains the terminal.
type actionFinishedMsg struct{ err error }

// Init implements tea.Model.
func (m *DashboardModel) Init() tea.Cmd {
	return nil
}

// Update implements tea.Model.
func (m *DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// A pending kill confirmation takes over key handling: y confirms, any
		// other key cancels.
		if m.pendingKill != "" {
			id := m.pendingKill
			m.pendingKill = ""
			if msg.String() == "y" {
				return m, m.runChild("kill", id, "--force")
			}
			return m, nil
		}
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
				// Switching is the one action that leaves the dashboard for the
				// selected session.
				return m, m.switchTo(m.sessions[m.cursor].ID)
			}
		case "n":
			// Create a project, then return to the dashboard (no auto-switch).
			return m, m.runChild("new", "--no-switch")
		case "c":
			if m.cursor < len(m.sessions) {
				return m, m.runChild("new", "--worktree", m.sessions[m.cursor].ID, "--no-switch")
			}
		case "d":
			if m.cursor < len(m.sessions) {
				// Kill removes worktrees and the tmux session, so confirm before
				// launching the child (which runs `eme kill <id> --force`).
				m.pendingKill = m.sessions[m.cursor].ID
				m.notice = ""
			}
		case "a":
			if m.cursor < len(m.sessions) {
				return m, m.runChild("agent", m.sessions[m.cursor].ID)
			}
		}
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case actionFinishedMsg:
		m.refresh(msg.err)
	}
	return m, nil
}

// View implements tea.Model.
func (m *DashboardModel) View() string {
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
	switch {
	case m.pendingKill != "":
		b += errorStyle.Render(fmt.Sprintf("kill %s?  y = confirm, any other key = cancel", m.killTargetName())) + "\n"
	case m.notice != "":
		b += errorStyle.Render(m.notice) + "\n"
	}
	if m.showHelp {
		b += helpStyle.Render("n: new  c: create worktree  enter/o: open  d: kill  a: agent  q: quit  ?: help") + "\n"
	} else {
		b += helpStyle.Render("?: help") + "\n"
	}
	return b
}

// killTargetName returns the display name of the session awaiting kill
// confirmation, falling back to its ID if it is no longer in the list.
func (m *DashboardModel) killTargetName() string {
	for _, s := range m.sessions {
		if s.ID == m.pendingKill {
			return s.DisplayName
		}
	}
	return m.pendingKill
}

// refresh re-reads the session list after a child action returns, recording any
// error as a transient notice. It never quits the dashboard, so a cancelled or
// failed action simply leaves the user back on the dashboard.
func (m *DashboardModel) refresh(actionErr error) {
	if actionErr != nil {
		m.notice = actionErr.Error()
	} else {
		m.notice = ""
	}
	if m.reload == nil {
		return
	}
	sessions, err := m.reload()
	if err != nil {
		m.notice = "refresh failed: " + err.Error()
		return
	}
	m.sessions = sessions
	if m.cursor >= len(m.sessions) {
		m.cursor = len(m.sessions) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// runChild runs `eme <args...>` as a child process, pausing the dashboard and
// handing it the terminal so its own picker/prompt renders, then resumes the
// dashboard and refreshes the session list. Used for create/kill/agent so the
// dashboard persists across them.
func (m *DashboardModel) runChild(args ...string) tea.Cmd {
	binary, err := os.Executable()
	if err != nil {
		return func() tea.Msg {
			return actionFinishedMsg{err: fmt.Errorf("locate eme binary: %w", err)}
		}
	}
	return tea.ExecProcess(exec.Command(binary, args...), func(err error) tea.Msg {
		return actionFinishedMsg{err: err}
	})
}

// switchTo replaces this process with `eme switch <id>`, leaving the dashboard
// for the target session. On success it never returns; the returned message is
// only delivered if exec itself fails.
func (m *DashboardModel) switchTo(id string) tea.Cmd {
	return func() tea.Msg {
		binary, err := os.Executable()
		if err != nil {
			return actionFinishedMsg{err: fmt.Errorf("locate eme binary: %w", err)}
		}
		err = syscall.Exec(binary, []string{"eme", "switch", id}, os.Environ())
		return actionFinishedMsg{err: fmt.Errorf("exec eme switch: %w", err)}
	}
}
