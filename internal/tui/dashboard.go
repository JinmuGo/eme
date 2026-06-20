// Package tui implements the eme terminal user interface.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jinmu/eme/internal/tui/theme"
)

// Styles map DESIGN.md roles to lipgloss. titleStyle, cursorStyle, mutedStyle,
// errorStyle, helpStyle are SHARED with picker.go / input.go / agentpicker.go
// (same package) and MUST remain defined here — do not drop them.
//
// One rule governs the palette: amber (theme.Beacon) is reserved for "the chosen
// one." Everything else is neutral; only the beacon and danger spend saturation.
var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(theme.Text) // wordmark stays neutral in-TUI; amber is the beacon
	cursorStyle = lipgloss.NewStyle().Bold(true).Foreground(theme.Text)
	mutedStyle  = lipgloss.NewStyle().Foreground(theme.Muted)
	errorStyle  = lipgloss.NewStyle().Foreground(theme.Danger)
	helpStyle   = lipgloss.NewStyle().Foreground(theme.Muted)

	textStyle     = lipgloss.NewStyle().Foreground(theme.Text)
	rhymeStyle    = lipgloss.NewStyle().Foreground(theme.Muted)
	needsYouStyle = lipgloss.NewStyle().Bold(true).Foreground(theme.Beacon)
	sessionStyle  = lipgloss.NewStyle().Bold(true).Foreground(theme.Text)
	rootStyle     = lipgloss.NewStyle().Foreground(theme.Muted)
	branchStyle   = lipgloss.NewStyle().Foreground(theme.Muted)
	addStyle      = lipgloss.NewStyle().Foreground(theme.Muted) // an addition is not an alert
	delStyle      = lipgloss.NewStyle().Foreground(theme.Danger)
	agentStyle    = lipgloss.NewStyle().Foreground(theme.Muted)

	// selectedGutter marks the cursor row with a quiet, non-hue ▌ on the surface
	// lift. Selection is a separate channel from the beacon: a background platform,
	// never a hue, so per-status foregrounds (the amber ●) survive under the cursor.
	selectedGutter = lipgloss.NewStyle().Foreground(theme.Muted).Background(theme.Surface)
	// panelStyle is the rounded border wrapping the whole dashboard.
	panelStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(theme.Border).
			Padding(0, 1)
)

// rowRef points at a worktree within the view-model.
type rowRef struct{ session, worktree int }

// killTarget describes a pending kill confirmation.
type killTarget struct {
	sessionID    string
	worktreeName string
	label        string
	isMain       bool
}

// DashboardModel is the main dashboard.
type DashboardModel struct {
	views    []SessionView
	rows     []rowRef // flattened selectable worktree rows, in render order
	cursor   int      // index into rows
	width    int
	height   int
	notice   string
	pending  *killTarget
	showHelp bool
	// leaving records that the user chose to switch (Enter) to leaveSession/
	// leaveWorktree. When true, the model has quit and the cmd layer execs
	// `eme switch` afterward, once bubbletea has restored the terminal. An
	// explicit flag (not an empty-string check) keeps this independent of how
	// session IDs are formed.
	leaving       bool
	leaveSession  string
	leaveWorktree string
	// reload re-reads the view-model after a child action returns. May be nil
	// (tests), in which case the list is not refreshed.
	reload func() ([]SessionView, error)
}

// NewDashboard creates a dashboard model. reload is called after each child
// action (create/kill/agent) completes to refresh the view-model.
func NewDashboard(views []SessionView, reload func() ([]SessionView, error)) *DashboardModel {
	m := &DashboardModel{views: views, reload: reload}
	m.rebuildRows()
	return m
}

// rebuildRows recomputes the flattened selectable list and clamps the cursor.
func (m *DashboardModel) rebuildRows() {
	m.rows = nil
	for si := range m.views {
		for wi := range m.views[si].Worktrees {
			m.rows = append(m.rows, rowRef{session: si, worktree: wi})
		}
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// selected returns the worktree under the cursor, or nil if the list is empty.
func (m *DashboardModel) selected() *WorktreeView {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	r := m.rows[m.cursor]
	return &m.views[r.session].Worktrees[r.worktree]
}

// needsYouCount counts worktrees whose status warrants attention.
func (m *DashboardModel) needsYouCount() int {
	n := 0
	for si := range m.views {
		for _, w := range m.views[si].Worktrees {
			if w.Status.NeedsAttention() {
				n++
			}
		}
	}
	return n
}

// actionFinishedMsg is delivered after a child `eme` process exits.
type actionFinishedMsg struct{ err error }

// Init implements tea.Model.
func (m *DashboardModel) Init() tea.Cmd { return nil }

// Update implements tea.Model.
func (m *DashboardModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.pending != nil {
			t := m.pending
			m.pending = nil
			if msg.String() == "y" {
				if t.isMain {
					return m, m.runChild("kill", t.sessionID, "--force")
				}
				return m, m.runChild("kill", t.sessionID, t.worktreeName, "--force")
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
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case "enter", "o":
			if w := m.selected(); w != nil {
				// Record the target and quit cleanly; the cmd layer execs
				// `eme switch` after bubbletea restores the terminal, so the
				// shell is never left in raw/alt-screen state.
				m.leaving = true
				m.leaveSession, m.leaveWorktree = w.SessionID, w.Name
				return m, tea.Quit
			}
		case "n":
			return m, m.runChild("new", "--no-switch")
		case "c":
			if w := m.selected(); w != nil {
				return m, m.runChild("new", "--worktree", w.SessionID, "--no-switch")
			}
		case "a":
			if args, ok := m.AgentArgs(false); ok {
				return m, m.runChild(args...)
			}
		case "A":
			if args, ok := m.AgentArgs(true); ok {
				return m, m.runChild(args...)
			}
		case "d":
			if w := m.selected(); w != nil {
				t := &killTarget{sessionID: w.SessionID, worktreeName: w.Name, isMain: w.IsMain}
				if w.IsMain {
					t.label = "project " + m.views[m.rows[m.cursor].session].DisplayName
				} else {
					t.label = "worktree " + w.Name
				}
				m.pending = t
				m.notice = ""
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

// View implements tea.Model. It renders a single rounded-border panel: a header
// (branding + rhyme on the left, the "N needs you" counter on the right), a
// session → worktree tree whose rows lead with agent status, and a footer pinned
// to the bottom. The worktree under the cursor is a full-width highlight bar.
func (m *DashboardModel) View() string {
	width, height := m.width, m.height
	if width < 40 {
		width = 80 // before the first WindowSizeMsg
	}
	if height < 10 {
		height = 24
	}
	boxWidth := width - 2 // total minus the left/right border columns
	inner := width - 4    // text area inside the border + horizontal padding
	if inner < 24 {
		inner = 24
	}
	innerHeight := height - 2 // minus the top/bottom border rows

	var lines []string

	// Header: branding + rhyme (left), "N needs you" (right), then a rule.
	left := titleStyle.Render("eme") + "  " + rhymeStyle.Render("eeny · meeny · miny · moe")
	right := ""
	if n := m.needsYouCount(); n > 0 {
		right = needsYouStyle.Render(fmt.Sprintf("%d needs you", n))
	}
	lines = append(lines, fitLine(left, right, inner))
	lines = append(lines, mutedStyle.Render(strings.Repeat("─", inner)))

	// Tree body.
	if len(m.rows) == 0 {
		lines = append(lines, "", mutedStyle.Render("No sessions. Press 'n' to create one."))
	} else {
		rowi := 0
		for si := range m.views {
			sv := m.views[si]
			head := " " + sessionStyle.Render(fmt.Sprintf("%d  %s", si+1, sv.DisplayName))
			rootStr := sv.Root
			if rootMax := inner - lipgloss.Width(head) - 1; rootMax > 1 {
				rootStr = truncate(sv.Root, rootMax)
			}
			lines = append(lines, fitLine(head, rootStyle.Render(rootStr), inner))
			for wi := range sv.Worktrees {
				lines = append(lines, m.worktreeLine(sv.Worktrees[wi], rowi == m.cursor, inner))
				rowi++
			}
			lines = append(lines, "")
		}
	}

	// Bottom block: a transient notice/confirm line then the footer, pinned to
	// the panel's last rows.
	var bottom []string
	if m.pending != nil {
		bottom = append(bottom, errorStyle.Render("kill "+m.pending.label+"?  y = confirm · any other key = cancel"))
	} else if m.notice != "" {
		bottom = append(bottom, errorStyle.Render(m.notice))
	}
	if m.showHelp {
		bottom = append(bottom, helpStyle.Render("↑↓/jk move · ↵/o open · n new · c worktree · a agent · A pick · d kill · q quit · ?"))
	} else {
		bottom = append(bottom, helpStyle.Render("↑↓ move · ↵ open · n new · d kill · ? more · q quit"))
	}
	for len(lines)+len(bottom) < innerHeight {
		lines = append(lines, "")
	}
	lines = append(lines, bottom...)

	return panelStyle.Width(boxWidth).Render(strings.Join(lines, "\n"))
}

// fitLine places left at the start and right-aligns right within width columns,
// measuring display width so ANSI styling does not skew the gap.
func fitLine(left, right string, width int) string {
	gap := width - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		gap = 1
	}
	return left + strings.Repeat(" ", gap) + right
}

// worktreeLine renders one worktree row, status-first. Columns are padded before
// styling so they stay aligned. The cursor row gets a neutral surface lift and a
// quiet ▌ gutter; critically, each cell keeps its own foreground so the amber
// beacon (and every status hue) survives under the cursor — selection and
// attention are separate channels (DESIGN.md §5.3).
func (m *DashboardModel) worktreeLine(w WorktreeView, selected bool, inner int) string {
	statusRaw := fmt.Sprintf("%s %-8s", w.Status.Glyph(), w.Status.Label())
	nameRaw := fmt.Sprintf("%-14s", truncate(w.Name, 14))
	branchRaw := fmt.Sprintf("%-16s", truncate(w.Branch, 16))

	// bg paints the surface lift on the cursor row and is a no-op elsewhere.
	// Applying it to every cell and gap keeps the platform continuous beneath the
	// per-cell foreground colors.
	bg := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(theme.Surface)
		}
		return s
	}
	plain := lipgloss.NewStyle()
	sep := bg(plain).Render("  ")

	var trailerCell string
	if w.AgentLabel != "" {
		trailerCell = bg(agentStyle).Render(w.AgentLabel)
	} else if w.HasDiff {
		trailerCell = bg(addStyle).Render(fmt.Sprintf("+%d", w.Added)) + bg(plain).Render(" ") + bg(delStyle).Render(fmt.Sprintf("-%d", w.Deleted))
	}

	gutter := bg(plain).Render("  ")
	if selected {
		gutter = selectedGutter.Render("▌") + bg(plain).Render(" ")
	}

	row := gutter +
		bg(statusStyle[w.Status]).Render(statusRaw) + sep +
		bg(textStyle).Render(nameRaw) + sep +
		bg(branchStyle).Render(branchRaw) + sep +
		trailerCell

	if selected {
		if pad := inner - lipgloss.Width(row); pad > 0 {
			row += bg(plain).Render(strings.Repeat(" ", pad))
		}
	}
	return row
}

// truncate shortens s to at most max display columns, adding an ellipsis.
func truncate(s string, max int) string {
	if max < 1 {
		return ""
	}
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	return string(r[:max-1]) + "…"
}

// refresh re-reads the view-model after a child action, recording any error as a
// transient notice. It never quits the dashboard.
func (m *DashboardModel) refresh(actionErr error) {
	if actionErr != nil {
		m.notice = actionErr.Error()
	} else {
		m.notice = ""
	}
	if m.reload == nil {
		return
	}
	views, err := m.reload()
	if err != nil {
		m.notice = "refresh failed: " + err.Error()
		return
	}
	m.views = views
	m.rebuildRows()
}

// AgentArgs returns the `eme agent …` child argv for the selected worktree, or
// ok=false when nothing is selected. pick appends --pick to open the catalog.
func (m *DashboardModel) AgentArgs(pick bool) ([]string, bool) {
	w := m.selected()
	if w == nil {
		return nil, false
	}
	args := []string{"agent", w.SessionID, w.Name}
	if pick {
		args = append(args, "--pick")
	}
	return args, true
}

// runChild runs `eme <args...>` as a child process, pausing the dashboard and
// handing it the terminal, then resumes and refreshes.
func (m *DashboardModel) runChild(args ...string) tea.Cmd {
	binary, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return actionFinishedMsg{err: fmt.Errorf("locate eme binary: %w", err)} }
	}
	return tea.ExecProcess(exec.Command(binary, args...), func(err error) tea.Msg {
		return actionFinishedMsg{err: err}
	})
}

// SwitchTarget reports the worktree the user chose to switch to with Enter, if
// any. The dashboard records it and quits; the caller execs `eme switch` once
// bubbletea has restored the terminal.
func (m *DashboardModel) SwitchTarget() (session, worktree string, ok bool) {
	if !m.leaving {
		return "", "", false
	}
	return m.leaveSession, m.leaveWorktree, true
}
