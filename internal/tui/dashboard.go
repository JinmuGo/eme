// Package tui implements the eme terminal user interface.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

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

// rowKind distinguishes a session header row from a worktree row in the flattened,
// fold-aware selectable list.
type rowKind int

const (
	rowSession rowKind = iota
	rowWorktree
)

// rowRef points at a row within the view-model: either a session header
// (kind==rowSession) or a worktree under it (kind==rowWorktree, worktree valid).
type rowRef struct {
	kind     rowKind
	session  int
	worktree int
}

// killTarget describes a pending kill confirmation.
type killTarget struct {
	sessionID    string
	worktreeName string
	label        string
	isMain       bool
}

// DashboardModel is the main dashboard.
type DashboardModel struct {
	views  []SessionView
	rows   []rowRef // flattened selectable rows (session headers + worktrees), in render order
	cursor int      // index into rows
	// collapsed records which sessions are folded, keyed by session identity (its
	// SessionID) so the fold state survives reloads and reorders — the same identity
	// principle the sticky cursor uses (ARCH-5).
	collapsed map[string]bool
	width     int
	height    int
	notice    string
	pending   *killTarget
	showHelp  bool
	// leaving records that the user chose to switch (Enter) to leaveSession/
	// leaveWorktree. When true, the model has quit and the cmd layer execs
	// `eme switch` afterward, once bubbletea has restored the terminal. An
	// explicit flag (not an empty-string check) keeps this independent of how
	// session IDs are formed.
	leaving       bool
	leaveSession  string
	leaveWorktree string
	// reload re-reads the FULL view-model (status + git diff, via reconcile) after a
	// child action returns. May be nil (tests), in which case the list is not refreshed.
	reload func() ([]SessionView, error)
	// statusReload is the cheap status-only reload the auto-refresh ticker uses (raw
	// state + snapshot, no git diff / reconcile). Installed via SetStatusReload; when
	// nil the ticker is inert.
	statusReload func() ([]SessionView, error)
	// peek captures the selected pane's last lines on demand (read-only). Installed
	// via SetPeek; when nil the `p` peek is inert. peeking/peekLines/peekLabel hold
	// the current on-demand peek — a momentary glance, not a standing panel, so they
	// are cleared the moment the cursor moves (DESIGN §5.7).
	peek      func(sessionID, worktreeName string) ([]string, error)
	peeking   bool
	peekLines []string
	peekLabel string
}

// NewDashboard creates a dashboard model. reload is called after each child
// action (create/kill/agent) completes to refresh the view-model.
func NewDashboard(views []SessionView, reload func() ([]SessionView, error)) *DashboardModel {
	m := &DashboardModel{views: views, reload: reload, collapsed: map[string]bool{}}
	m.rebuildRows()
	return m
}

// rebuildRows recomputes the flattened selectable list — a header per session, then
// that session's worktrees unless it is folded — and clamps the cursor.
func (m *DashboardModel) rebuildRows() {
	m.rows = nil
	for si := range m.views {
		m.rows = append(m.rows, rowRef{kind: rowSession, session: si})
		if m.isCollapsed(si) {
			continue
		}
		for wi := range m.views[si].Worktrees {
			m.rows = append(m.rows, rowRef{kind: rowWorktree, session: si, worktree: wi})
		}
	}
	if m.cursor >= len(m.rows) {
		m.cursor = len(m.rows) - 1
	}
	if m.cursor < 0 {
		m.cursor = 0
	}
}

// sessionKey is the stable identity used to track a session's fold state across
// reloads. Sessions are keyed by their worktrees' SessionID (unique); the display
// name is a fallback for the unexpected empty session.
func sessionKey(sv SessionView) string {
	if len(sv.Worktrees) > 0 {
		return sv.Worktrees[0].SessionID
	}
	return sv.DisplayName
}

// isCollapsed reports whether the session at index si is folded.
func (m *DashboardModel) isCollapsed(si int) bool {
	return m.collapsed[sessionKey(m.views[si])]
}

// setCollapsed sets the fold state for the session at index si.
func (m *DashboardModel) setCollapsed(si int, v bool) {
	if m.collapsed == nil {
		m.collapsed = map[string]bool{}
	}
	m.collapsed[sessionKey(m.views[si])] = v
}

// currentRow returns the row under the cursor, or nil if the list is empty.
func (m *DashboardModel) currentRow() *rowRef {
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		return nil
	}
	return &m.rows[m.cursor]
}

// selected returns the worktree under the cursor, or nil when the list is empty or
// the cursor rests on a session header.
func (m *DashboardModel) selected() *WorktreeView {
	r := m.currentRow()
	if r == nil || r.kind != rowWorktree {
		return nil
	}
	return &m.views[r.session].Worktrees[r.worktree]
}

// selectedSession returns the session index under the cursor — valid on both header
// and worktree rows — or -1 when the list is empty.
func (m *DashboardModel) selectedSession() int {
	r := m.currentRow()
	if r == nil {
		return -1
	}
	return r.session
}

// headerIndexForSession returns the row index of a session's header.
func (m *DashboardModel) headerIndexForSession(si int) int {
	for i, r := range m.rows {
		if r.kind == rowSession && r.session == si {
			return i
		}
	}
	return m.cursor
}

// collapseSession folds a session and parks the cursor on its header so the row the
// user was on never vanishes beneath them.
func (m *DashboardModel) collapseSession(si int) {
	m.setCollapsed(si, true)
	m.rebuildRows()
	m.cursor = m.headerIndexForSession(si)
}

// expandSession unfolds a session, keeping the cursor on its header.
func (m *DashboardModel) expandSession(si int) {
	m.setCollapsed(si, false)
	m.rebuildRows()
	m.cursor = m.headerIndexForSession(si)
}

// toggleFold flips a session's fold state (Enter/o on a header).
func (m *DashboardModel) toggleFold(si int) {
	if m.isCollapsed(si) {
		m.expandSession(si)
	} else {
		m.collapseSession(si)
	}
}

// foldLeft implements h/←: on a worktree, collapse its parent and jump to the header;
// on an expanded header, collapse it; on a collapsed header, do nothing.
func (m *DashboardModel) foldLeft() {
	r := m.currentRow()
	if r == nil {
		return
	}
	if r.kind == rowWorktree || !m.isCollapsed(r.session) {
		m.collapseSession(r.session)
	}
}

// foldRightOrOpen implements l/→: expand a collapsed header, step into the first
// child of an expanded header, or open a worktree (same as Enter). It returns a
// tea.Cmd only when opening a worktree.
func (m *DashboardModel) foldRightOrOpen() tea.Cmd {
	r := m.currentRow()
	if r == nil {
		return nil
	}
	if r.kind == rowSession {
		if m.isCollapsed(r.session) {
			m.expandSession(r.session)
		} else if len(m.views[r.session].Worktrees) > 0 && m.cursor < len(m.rows)-1 {
			m.cursor++ // step into the first worktree
		}
		return nil
	}
	w := &m.views[r.session].Worktrees[r.worktree]
	m.leaving = true
	m.leaveSession, m.leaveWorktree = w.SessionID, w.Name
	return tea.Quit
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

// tickMsg drives the auto-refresh ticker.
type tickMsg struct{}

// refreshInterval is the dashboard's auto-refresh cadence. 2s matches the tmux
// status bar's status-interval, so the popup and the ambient segment stay in step,
// and is cheap because ticks take the status-only read path.
const refreshInterval = 2 * time.Second

// tick schedules the next auto-refresh.
func (m *DashboardModel) tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(time.Time) tea.Msg { return tickMsg{} })
}

// Init implements tea.Model. It starts the auto-refresh ticker so the beacon lights
// without a keypress.
func (m *DashboardModel) Init() tea.Cmd { return m.tick() }

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
			m.closePeek() // the peek belonged to the row we're leaving
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			m.closePeek()
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
		case "left", "h":
			m.closePeek()
			m.foldLeft()
		case "right", "l":
			m.closePeek()
			if cmd := m.foldRightOrOpen(); cmd != nil {
				return m, cmd
			}
		case "p":
			m.togglePeek()
		case "enter", "o":
			if r := m.currentRow(); r != nil && r.kind == rowSession {
				m.closePeek()
				m.toggleFold(r.session)
			} else if w := m.selected(); w != nil {
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
			// Create a worktree in the session under the cursor (header or worktree).
			if si := m.selectedSession(); si >= 0 {
				return m, m.runChild("new", "--worktree", sessionKey(m.views[si]), "--no-switch")
			}
		case "a":
			if args, ok := m.AgentArgs(false); ok {
				return m, m.runChild(args...)
			}
		case "A":
			if args, ok := m.AgentArgs(true); ok {
				return m, m.runChild(args...)
			}
		case "x":
			// Clear a finished agent's frozen pane back to idle. Gated to dead-pane
			// statuses so it never disturbs a live or never-run worktree; `eme clean`
			// guards again on its own. The refresh after the child shows it idle.
			if w := m.selected(); w != nil && (w.Status == StatusCrashed || w.Status == StatusExited) {
				return m, m.runChild("clean", w.SessionID, w.Name)
			}
		case "d":
			if r := m.currentRow(); r != nil && r.kind == rowSession {
				sv := m.views[r.session]
				m.pending = &killTarget{sessionID: sessionKey(sv), isMain: true, label: "project " + sv.DisplayName}
				m.notice = ""
			} else if w := m.selected(); w != nil {
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
	case tickMsg:
		m.tickReload()
		return m, m.tick()
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

	// Tree body. Iterate the flattened rows so the fold state and the cursor
	// highlight read identically on session headers and worktrees.
	if len(m.rows) == 0 {
		lines = append(lines, "", mutedStyle.Render("No sessions. Press 'n' to create one."))
	} else {
		for i, r := range m.rows {
			switch r.kind {
			case rowSession:
				if i > 0 {
					lines = append(lines, "") // breathe between sessions
				}
				lines = append(lines, m.sessionLine(r.session, i == m.cursor, inner))
			case rowWorktree:
				lines = append(lines, m.worktreeLine(m.views[r.session].Worktrees[r.worktree], i == m.cursor, inner))
			}
		}
	}

	// Bottom block: a transient notice/confirm line then the footer, pinned to
	// the panel's last rows.
	var bottom []string
	if m.peeking {
		bottom = append(bottom, m.peekBlock(inner)...)
	}
	if m.pending != nil {
		bottom = append(bottom, errorStyle.Render("kill "+m.pending.label+"?  y = confirm · any other key = cancel"))
	} else if m.notice != "" {
		bottom = append(bottom, errorStyle.Render(m.notice))
	}
	if m.showHelp {
		bottom = append(bottom, helpStyle.Render("↑↓/jk move · ←→/hl fold · ↵/o open · p peek · n new · c worktree · a agent · A pick · x clean · d kill · q quit · ?"))
	} else {
		bottom = append(bottom, helpStyle.Render("↑↓/jk move · ←→/hl fold · ↵ open · n new · d kill · ? more · q quit"))
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

// sessionLine renders one session header row: a fold caret (▾ open · ▸ folded), the
// session ordinal and name, and its root path right-aligned (with a hidden-count tail
// when folded). Like worktreeLine it carries the cursor's surface lift and ▌ gutter,
// so selection reads the same on a header as on a worktree (DESIGN.md §5.3).
func (m *DashboardModel) sessionLine(si int, selected bool, inner int) string {
	sv := m.views[si]
	bg := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(theme.Surface)
		}
		return s
	}
	plain := lipgloss.NewStyle()

	gutter := bg(plain).Render("  ")
	if selected {
		gutter = selectedGutter.Render("▌") + bg(plain).Render(" ")
	}

	caret := "▾"
	if m.isCollapsed(si) {
		caret = "▸"
	}
	head := gutter + bg(rhymeStyle).Render(caret) + bg(plain).Render(" ") +
		bg(sessionStyle).Render(fmt.Sprintf("%d  %s", si+1, sv.DisplayName))

	tail := sv.Root
	if m.isCollapsed(si) {
		if n := len(sv.Worktrees); n > 0 {
			tail = fmt.Sprintf("%s  (%d hidden)", sv.Root, n)
		}
	}
	tailCell := bg(rootStyle).Render("")
	if rootMax := inner - lipgloss.Width(head) - 1; rootMax > 1 {
		tailCell = bg(rootStyle).Render(truncate(tail, rootMax))
	}

	gap := inner - lipgloss.Width(head) - lipgloss.Width(tailCell)
	if gap < 1 {
		gap = 1
	}
	row := head + bg(plain).Render(strings.Repeat(" ", gap)) + tailCell
	if selected {
		if pad := inner - lipgloss.Width(row); pad > 0 {
			row += bg(plain).Render(strings.Repeat(" ", pad))
		}
	}
	return row
}

// peekBlock renders the on-demand peek: a quiet rule, a label, then the captured
// last lines — all muted so the peek stays subordinate to the tree and never
// competes with the beacon. Rendered only while peeking, so it spends zero rows
// otherwise (DESIGN §5.7).
func (m *DashboardModel) peekBlock(inner int) []string {
	out := []string{
		mutedStyle.Render(strings.Repeat("─", inner)),
		mutedStyle.Render("peek " + m.peekLabel),
	}
	if len(m.peekLines) == 0 {
		return append(out, mutedStyle.Render("  (no output)"))
	}
	for _, ln := range m.peekLines {
		out = append(out, mutedStyle.Render("  "+truncate(ln, inner-2)))
	}
	return out
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
	m.applyViews(views)
}

// SetStatusReload installs the cheap status-only reload the auto-refresh ticker uses
// (raw state + snapshot, no git diff / reconcile). Separate from the full post-action
// reload so ticks stay cheap.
func (m *DashboardModel) SetStatusReload(fn func() ([]SessionView, error)) {
	m.statusReload = fn
}

// SetPeek installs the read-only pane-capture used by the `p` peek. When nil the
// peek is inert (e.g. in tests).
func (m *DashboardModel) SetPeek(fn func(sessionID, worktreeName string) ([]string, error)) {
	m.peek = fn
}

// togglePeek opens the on-demand peek for the selected worktree, or closes it if
// already open. The capture is read once (a momentary glance, not a live tail); a
// failure surfaces as a transient notice and leaves the peek closed.
func (m *DashboardModel) togglePeek() {
	if m.peeking {
		m.closePeek()
		return
	}
	w := m.selected()
	if w == nil || m.peek == nil {
		return
	}
	lines, err := m.peek(w.SessionID, w.Name)
	if err != nil {
		m.notice = "peek failed: " + err.Error()
		return
	}
	m.peeking = true
	m.peekLines = lines
	m.peekLabel = w.Name
}

// closePeek clears the peek so no rows are spent when not peeking.
func (m *DashboardModel) closePeek() {
	m.peeking = false
	m.peekLines = nil
	m.peekLabel = ""
}

// tickReload refreshes agent status from the cheap snapshot path on each tick,
// carrying the last-known diff forward (the status path skips git diff) and keeping
// the cursor sticky. A transient read failure is silent — last-known views are kept,
// never a guessed status (F1).
func (m *DashboardModel) tickReload() {
	if m.statusReload == nil {
		return
	}
	views, err := m.statusReload()
	if err != nil {
		return
	}
	carryDiffStats(views, m.views)
	m.applyViews(views)
}

// applyViews swaps in a fresh view-model while keeping the cursor on the same row by
// identity — a session header by its session key, a worktree by (session, worktree) —
// so an auto-refresh never makes the selection jump under the user (ARCH-5). Falls
// back to the clamped index (from rebuildRows) when the row is gone.
func (m *DashboardModel) applyViews(views []SessionView) {
	var wantSession string       // set when the cursor was on a session header
	var wantSID, wantName string // set when the cursor was on a worktree
	if r := m.currentRow(); r != nil {
		if r.kind == rowSession {
			wantSession = sessionKey(m.views[r.session])
		} else {
			w := m.views[r.session].Worktrees[r.worktree]
			wantSID, wantName = w.SessionID, w.Name
		}
	}
	m.views = views
	m.rebuildRows()
	for i, r := range m.rows {
		switch r.kind {
		case rowSession:
			if wantSession != "" && sessionKey(m.views[r.session]) == wantSession {
				m.cursor = i
				return
			}
		case rowWorktree:
			if wantSID != "" {
				if w := m.views[r.session].Worktrees[r.worktree]; w.SessionID == wantSID && w.Name == wantName {
					m.cursor = i
					return
				}
			}
		}
	}
}

// carryDiffStats copies the diff columns from src into dst by worktree identity, so
// the cheap status-only tick path (which skips git diff) does not blank a worktree's
// +N/-M between full reloads.
func carryDiffStats(dst, src []SessionView) {
	type key struct{ sid, name string }
	prev := make(map[key]WorktreeView)
	for si := range src {
		for _, w := range src[si].Worktrees {
			prev[key{w.SessionID, w.Name}] = w
		}
	}
	for si := range dst {
		for wi := range dst[si].Worktrees {
			w := &dst[si].Worktrees[wi]
			if p, ok := prev[key{w.SessionID, w.Name}]; ok {
				w.Added, w.Deleted, w.HasDiff = p.Added, p.Deleted, p.HasDiff
			}
		}
	}
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
