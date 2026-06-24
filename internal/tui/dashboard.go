// Package tui implements the eme terminal user interface.
package tui

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/jinmu/eme/internal/errors"
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

	textStyle       = lipgloss.NewStyle().Foreground(theme.Text)
	rhymeStyle      = lipgloss.NewStyle().Foreground(theme.Muted)
	needsYouStyle   = lipgloss.NewStyle().Bold(true).Foreground(theme.Beacon)
	sessionStyle    = lipgloss.NewStyle().Bold(true).Foreground(theme.Text)
	rootStyle       = lipgloss.NewStyle().Foreground(theme.Muted)
	branchStyle     = lipgloss.NewStyle().Foreground(theme.Muted)
	locationStyle   = lipgloss.NewStyle().Foreground(theme.Muted) // worktree dir; reference info, no hue
	caffeinateStyle = lipgloss.NewStyle().Foreground(theme.Working)
	// ageStyle renders the age cell as muted reference info — a temporal qualifier on the
	// status, never a hue of its own (DESIGN §5.3: age is chrome, not a signal channel).
	ageStyle = lipgloss.NewStyle().Foreground(theme.Idle)

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

// Worktree-row column geometry, shared by worktreeLine and schemaLine so the data rows
// and the column-label row never drift out of alignment.
const (
	colGutterW = 2  // cursor gutter ("▌ " or "  ")
	colStatusW = 10 // status glyph + space + label padded to 8
	colAgeW    = 4  // compact age in state ("12m"); blank for non-hooked/idle rows
	colNameW   = 14
	colBranchW = 16
	colSep     = "  "
	// wtPrefixW is the width consumed before the trailing location column.
	wtPrefixW = colGutterW + colStatusW + len(colSep) + colAgeW + len(colSep) + colNameW + len(colSep) + colBranchW + len(colSep)
)

// rowKind distinguishes a session header row from a worktree row in the flattened,
// fold-aware selectable list.
type rowKind int

const (
	rowSession rowKind = iota
	rowWorktree
)

// previewMinWidth is the minimum terminal width at which the P side preview panel is
// allowed to open. Below this a split would crush both the tree and the preview pane.
const previewMinWidth = 72

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
	// escalated marks the second-stage project confirm shown after a plain delete was
	// refused because the project's history is on no remote. It swaps the affirmative
	// key to D (delete anyway → --force-unpushed) so the louder option only ever appears
	// when the guard actually fired.
	escalated bool
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
	// sortByAttention floats waiting/crashed/quiet worktrees to the top WITHIN each session
	// (off by default, toggled by `s`). rebuildRows is the single ordering authority, so the
	// mode survives every reload/tick automatically; applyViews keeps the cursor on identity.
	sortByAttention bool
	width           int
	height          int
	notice          string
	pending         *killTarget
	// lastDelete records the project a plain delete was just dispatched for, so an
	// unpushed-history refusal coming back from the child can be turned into the
	// escalated "delete anyway" confirm instead of a dead-end error notice.
	lastDelete *killTarget
	showHelp   bool
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
	// preview is a persistent side panel (p) showing the selected agent's live output,
	// re-captured on cursor move and on each tick — a babysit-one-agent panel that follows
	// the cursor. previewCapture reads the FULL pane (the box clamps to its height).
	preview        bool
	previewLines   []string
	previewLabel   string
	previewSID     string
	previewCapture func(sessionID, worktreeName string) ([]string, error)
	// modal, when non-nil, is an in-dashboard dialog (worktree-name input, agent picker, or
	// folder picker) drawn centered over the frozen tree via overlayCenter — so an action's
	// prompt never clears the dashboard. While it is open it owns the keyboard; flow holds
	// the multi-step context that decides what completing it runs. The pickers are built by
	// injected factories so the cmd layer keeps the agent catalog and folder scan, leaving
	// tui free of that knowledge; when a factory is nil the action falls back to a child
	// process (runChild), preserving the pre-modal behavior.
	modal            overlayModal
	flow             *modalFlow
	makeAgentPicker  func(sessionID, worktreeName string) *AgentPickerModel
	makeFolderPicker func() *FolderPickerModel
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
		for _, wi := range m.worktreeOrder(si) {
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

// worktreeOrder returns session si's worktree indices in render order: state order by
// default, or attention-first when sortByAttention is on — crashed < waiting < quiet <
// working < idle < exited, then longest-in-state first (oldest StateChangedAt; unknown
// age last). Stable, and never mutates the view-model.
func (m *DashboardModel) worktreeOrder(si int) []int {
	wts := m.views[si].Worktrees
	idx := make([]int, len(wts))
	for i := range idx {
		idx[i] = i
	}
	if !m.sortByAttention {
		return idx
	}
	sort.SliceStable(idx, func(a, b int) bool {
		wa, wb := wts[idx[a]], wts[idx[b]]
		ra, rb := attentionRank(wa.Status, wa.Quiet), attentionRank(wb.Status, wb.Quiet)
		if ra != rb {
			return ra < rb
		}
		return olderFirst(wa.StateChangedAt, wb.StateChangedAt)
	})
	return idx
}

// attentionRank orders worktrees so the ones that need you float up (lower = higher).
func attentionRank(s AgentStatus, quiet bool) int {
	switch {
	case s == StatusCrashed:
		return 0
	case s == StatusWaiting:
		return 1
	case quiet:
		return 2
	case s == StatusWorking:
		return 3
	case s == StatusIdle:
		return 4
	default:
		return 5
	}
}

// olderFirst reports whether a sorts before b for the age tiebreak: a known, older state
// change ranks above a newer one (longest-stuck first); an unknown (zero) time sorts last.
func olderFirst(a, b time.Time) bool {
	if a.IsZero() != b.IsZero() {
		return !a.IsZero()
	}
	if a.IsZero() {
		return false
	}
	return a.Before(b)
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

// caffeinateSupportedTUI gates the w-key feedback to macOS. A var seam for tests.
var caffeinateSupportedTUI = func() bool { return runtime.GOOS == "darwin" }

// nextCaffeinateMode cycles a session's keep-awake intent for the w key:
// off → manual → auto → off. The returned value is the --mode argument for the CLI
// ("off" maps back to "" intent inside `eme caffeinate`).
func nextCaffeinateMode(cur string) string {
	switch cur {
	case "manual":
		return "auto"
	case "auto":
		return "off"
	default: // "" (off)
		return "manual"
	}
}

// caffeinateBadge is the session-header marker for a keep-awake intent (ASCII, per the
// glyph convention). "" when off.
func caffeinateBadge(mode string) string {
	switch mode {
	case "manual":
		return "(caf)"
	case "auto":
		return "(caf~)"
	default:
		return ""
	}
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

// tally builds the header-right counter as two hue-correct segments: the waiting count
// in beacon and the crashed count in danger, joined by a muted " · " when both fire
// (DESIGN §5.2 — "the number is the same hue as the dots it counts"). Amber stays
// reserved for waiting; a crash spends danger, never the beacon. Empty when nothing
// waits or has crashed — the dark-cockpit ideal (no light = all fine).
func (m *DashboardModel) tally() string {
	var waiting, crashed int
	for si := range m.views {
		for _, w := range m.views[si].Worktrees {
			switch w.Status {
			case StatusWaiting:
				waiting++
			case StatusCrashed:
				crashed++
			}
		}
	}
	var parts []string
	if waiting > 0 {
		parts = append(parts, needsYouStyle.Render(fmt.Sprintf("%d waiting", waiting)))
	}
	if crashed > 0 {
		parts = append(parts, errorStyle.Render(fmt.Sprintf("%d crashed", crashed)))
	}
	return strings.Join(parts, mutedStyle.Render(" · "))
}

// actionFinishedMsg is delivered after a child `eme` process exits. output carries the
// child's combined output when it ran in the background (runChildBackground); it is empty
// for foreground children (runChild), whose output went straight to the terminal.
type actionFinishedMsg struct {
	err    error
	output string
}

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
	// An open modal owns input until it closes; route everything to it.
	if m.modal != nil {
		return m.updateWithModal(msg)
	}
	switch msg := msg.(type) {
	case tea.KeyMsg:
		if m.pending != nil {
			t := m.pending
			m.pending = nil
			args, ok := confirmArgs(t, msg.String())
			if !ok {
				return m, nil
			}
			// A plain project delete (y, not the escalated D) may bounce back refused
			// because the history is on no remote; remember the target so the child's
			// exit can be turned into the escalated confirm rather than a dead end.
			if t.isMain && !t.escalated && args[0] == "kill" {
				m.lastDelete = t
			}
			return m, m.runChild(args...)
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
			m.refreshPreview()
		case "down", "j":
			if m.cursor < len(m.rows)-1 {
				m.cursor++
			}
			m.refreshPreview()
		case "left", "h":
			m.foldLeft()
			m.refreshPreview()
		case "right", "l":
			if cmd := m.foldRightOrOpen(); cmd != nil {
				return m, cmd
			}
			m.refreshPreview()
		case "p":
			m.togglePreview()
		case "s":
			m.toggleSortMode()
		case "enter", "o":
			if r := m.currentRow(); r != nil && r.kind == rowSession {
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
			// New project: pick the folder, then the agent, both as in-dashboard modals.
			if !m.modalsWired() {
				return m, m.runChild("new", "--no-switch")
			}
			return m, m.openModal(m.makeFolderPicker(), &modalFlow{kind: flowNewProject})
		case "c":
			// Create a worktree in the session under the cursor (header or worktree).
			// A plain (non-git) folder has no git worktrees, so gate the action here:
			// give a one-line reason instead of spawning a child that can only fail.
			if si := m.selectedSession(); si >= 0 {
				if m.views[si].IsPlain {
					m.notice = m.views[si].DisplayName + " is a plain folder — no git worktrees (run `git init` to enable)."
					return m, nil
				}
				m.notice = ""
				sk := sessionKey(m.views[si])
				if !m.modalsWired() {
					return m, m.runChild("new", "--worktree", sk, "--no-switch")
				}
				return m, m.openModal(NewInput("Worktree name"), &modalFlow{kind: flowWorktree, sessKey: sk})
			}
		case "a":
			// Toggle the agent in the selected worktree — non-interactive, so run it in the
			// background instead of handing over the screen.
			if args, ok := m.AgentArgs(false); ok {
				return m, m.runChildBackground(args...)
			}
			m.notice = "select a worktree to run an agent (the cursor is on a session)"
		case "A":
			// Re-pick the agent for the selected worktree via an in-dashboard picker.
			w := m.selected()
			if w == nil {
				m.notice = "select a worktree to run an agent (the cursor is on a session)"
				break
			}
			if !m.modalsWired() {
				return m, m.runChild("agent", w.SessionID, w.Name, "--pick")
			}
			return m, m.openModal(m.makeAgentPicker(w.SessionID, w.Name), &modalFlow{kind: flowAgentOnly, sessKey: w.SessionID, wtName: w.Name})
		case "x":
			// Clear a finished agent's frozen pane back to idle. Gated to dead-pane
			// statuses so it never disturbs a live or never-run worktree; `eme clean`
			// guards again on its own. The refresh after the child shows it idle. The
			// gate gives a one-line reason instead of a silent no-op (advertised in help).
			if w := m.selected(); w == nil {
				m.notice = "select a finished worktree to clean"
			} else if w.Status == StatusCrashed || w.Status == StatusExited {
				return m, m.runChild("clean", w.SessionID, w.Name)
			} else {
				m.notice = "x clears a finished agent — " + w.Name + " is " + w.Status.Label()
			}
		case "w":
			// Cycle the session under the cursor through off → manual → auto → off.
			// caffeinate is session-scoped, so this works on a header or any worktree row.
			if !caffeinateSupportedTUI() {
				m.notice = "caffeinate is macOS-only"
				break
			}
			si := m.selectedSession()
			if si < 0 {
				m.notice = "select a session to toggle caffeinate"
				break
			}
			m.notice = ""
			return m, m.runChildBackground("caffeinate", sessionKey(m.views[si]), "--mode", nextCaffeinateMode(m.views[si].Caffeinate))
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
		// A just-dispatched plain delete that bounced back with the unpushed-history
		// exit code becomes a second, louder confirm (D = delete anyway) rather than a
		// dead-end "exit status 10" notice — the only in-UI path to the override.
		if d := m.lastDelete; d != nil {
			m.lastDelete = nil
			if isUnpushedExit(msg.err) {
				m.pending = &killTarget{sessionID: d.sessionID, label: d.label, isMain: true, escalated: true}
				m.notice = ""
				return m, nil
			}
		}
		m.refresh(msg.err, msg.output)
	case tickMsg:
		m.tickReload()
		return m, m.tick()
	}
	return m, nil
}

// View implements tea.Model. It renders a single rounded-border panel: a header
// (branding + rhyme on the left, the waiting/crashed tally on the right), a
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
	// Reserve the preview panel's width BEFORE computing boxWidth/inner so the whole
	// existing single-column render shrinks to the tree's column automatically.
	previewOn := m.preview && width >= previewMinWidth
	var previewW int
	if previewOn {
		previewW = width * 2 / 5 // ~40% to the preview
		if previewW < 28 {
			previewW = 28
		}
		if width-previewW < 40 { // never starve the tree
			previewOn = false
			previewW = 0
		}
	}
	treeWidth := width - previewW
	boxWidth := treeWidth - 2 // total minus the left/right border columns
	inner := treeWidth - 4    // text area inside the border + horizontal padding
	if inner < 24 {
		inner = 24
	}
	innerHeight := height - 2 // minus the top/bottom border rows

	// Header (2 rows): branding + rhyme (left), the waiting/crashed tally (right), a rule.
	left := titleStyle.Render("eme") + "  " + rhymeStyle.Render("eeny · meeny · miny · moe")
	if m.sortByAttention {
		left += "  " + mutedStyle.Render("· sort: attention")
	}
	header := []string{
		clampWidth(fitLine(left, m.tally(), inner), inner),
		clampWidth(mutedStyle.Render(strings.Repeat("─", inner)), inner),
	}
	if m.hasWorktree() {
		header = append(header, schemaLine(inner))
	}

	// Bottom block (notice/confirm + footer), built before the body so the body's row
	// budget can subtract it. Each entry is wrapped/clamped to a single terminal row so the
	// height accounting below counts real rows, not logical strings that may soft-wrap.
	var bottom []string
	if m.pending != nil {
		switch {
		case m.pending.escalated:
			bottom = append(bottom, wrapStyled(errorStyle, "delete "+m.pending.label+" anyway? its history is on no remote — D = delete anyway · f = forget (keep files) · any other key = cancel", inner)...)
		case m.pending.isMain:
			bottom = append(bottom, wrapStyled(errorStyle, "delete "+m.pending.label+"?  y = delete files · f = forget (keep files) · any other key = cancel", inner)...)
		default:
			bottom = append(bottom, wrapStyled(errorStyle, "kill "+m.pending.label+"?  y = confirm · any other key = cancel", inner)...)
		}
	} else if m.notice != "" {
		bottom = append(bottom, wrapStyled(errorStyle, m.notice, inner)...)
	}
	help := "↑↓/jk move · ←→/hl fold · ↵ open · n new · d kill · ? more · q quit"
	if m.showHelp {
		help = "↑↓/jk move · ←→/hl fold · ↵/o open · p preview · n new · c worktree · a agent · A pick · x clean · s sort · w wake · d kill · q quit · ?"
	}
	bottom = append(bottom, wrapStyled(helpStyle, help, inner)...)

	// Never let the bottom block crowd out the whole tree: keep at least one body row, and
	// if the block is itself taller than the popup, drop its leading rows (the confirm/notice)
	// so the footer — the last and most load-bearing line — always survives.
	if maxBottom := innerHeight - len(header) - 1; maxBottom >= 1 && len(bottom) > maxBottom {
		bottom = bottom[len(bottom)-maxBottom:]
	}

	// Tree body. Iterate the flattened rows so the fold state and the cursor highlight read
	// identically on session headers and worktrees; cursorLine records where the selected
	// row lands so the window can keep it in view.
	var body []string
	cursorLine := 0
	if len(m.rows) == 0 {
		body = append(body, "", clampWidth(mutedStyle.Render("No sessions. Press 'n' to create one."), inner))
	} else {
		for i, r := range m.rows {
			if r.kind == rowSession && i > 0 {
				body = append(body, "") // breathe between sessions
			}
			if i == m.cursor {
				cursorLine = len(body)
			}
			switch r.kind {
			case rowSession:
				body = append(body, clampWidth(m.sessionLine(r.session, i == m.cursor, inner), inner))
			case rowWorktree:
				body = append(body, clampWidth(m.worktreeLine(m.views[r.session].Worktrees[r.worktree], i == m.cursor, inner), inner))
			}
		}
	}

	// Window the body to the rows left after the header and bottom block, scrolling to keep
	// the cursor in view with a "↑/↓ N more" marker on each clipped side. Without this the
	// panel grows past the popup and tmux clips the closing border and the footer.
	bodyCap := innerHeight - len(header) - len(bottom)
	if bodyCap < 1 {
		bodyCap = 1
	}
	body = windowBody(body, cursorLine, bodyCap)

	lines := make([]string, 0, innerHeight)
	lines = append(lines, header...)
	lines = append(lines, body...)
	for len(lines)+len(bottom) < innerHeight {
		lines = append(lines, "")
	}
	lines = append(lines, bottom...)

	panel := panelStyle.Width(boxWidth).Render(strings.Join(lines, "\n"))
	if previewOn {
		panel = lipgloss.JoinHorizontal(lipgloss.Top, panel, m.previewBox(previewW, height))
	}
	// An open dialog is drawn centered over the (frozen) tree, so an action's prompt floats
	// on top of the dashboard instead of clearing it.
	if m.modal != nil {
		return overlayCenter(panel, m.modal.Box())
	}
	return panel
}

// clampWidth truncates s to at most width display columns, preserving ANSI styling, so a
// rendered line occupies exactly one terminal row and the height math stays honest.
func clampWidth(s string, width int) string {
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}

// wrapStyled renders raw text in style, word-wrapped to width, and returns it as
// individual one-row lines, so a long footer/notice spends a known, counted number of
// rows instead of silently wrapping past the popup.
func wrapStyled(style lipgloss.Style, text string, width int) []string {
	return strings.Split(style.Width(width).Render(text), "\n")
}

// windowBody returns at most capacity rows from body, centered on the cursor's line, with
// a muted "↑/↓ N more" marker replacing each clipped end. Every body line is already a
// single terminal row, so this is a plain line window: the tree scrolls, the panel never
// outgrows the popup. The cursor's line is always kept inside the visible band.
func windowBody(body []string, cursorLine, capacity int) []string {
	if capacity < 1 {
		capacity = 1
	}
	if len(body) <= capacity {
		return body
	}
	// Reserve a marker row only on a side that actually hides content: try two markers,
	// then one, then none, and use the first arrangement whose reserved count matches what
	// the clamped window really hides.
	for markers := 2; markers >= 0; markers-- {
		content := capacity - markers
		if content < 1 {
			continue
		}
		start := cursorLine - content/2
		if start < 0 {
			start = 0
		}
		if start > len(body)-content {
			start = len(body) - content
		}
		end := start + content
		top, bot := start > 0, end < len(body)
		used := 0
		if top {
			used++
		}
		if bot {
			used++
		}
		if used != markers {
			continue
		}
		out := make([]string, 0, capacity)
		if top {
			out = append(out, mutedStyle.Render(fmt.Sprintf("↑ %d more", start)))
		}
		out = append(out, body[start:end]...)
		if bot {
			out = append(out, mutedStyle.Render(fmt.Sprintf("↓ %d more", len(body)-end)))
		}
		return out
	}
	// No marker arrangement fits — the window is too small (1–2 rows, e.g. a short popup
	// with a confirm prompt up) to spare a row for a "more" hint. Fall back to a marker-less
	// window centered on the cursor so the selected row stays visible (we lose only the
	// hint, never the cursor) instead of anchoring at the top and scrolling it off-screen.
	start := cursorLine - capacity/2
	if start < 0 {
		start = 0
	}
	if start > len(body)-capacity {
		start = len(body) - capacity
	}
	return body[start : start+capacity]
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
	// labels are ASCII and glyphs are width-1, so byte-format padding == colStatusW.
	statusRaw := fmt.Sprintf("%s %-8s", w.Status.Glyph(), w.Status.Label())
	ageRaw := padCell(w.AgeLabel, colAgeW)
	nameRaw := padCell(w.Name, colNameW)
	branchRaw := padCell(w.Branch, colBranchW)

	bg := func(s lipgloss.Style) lipgloss.Style {
		if selected {
			return s.Background(theme.Surface)
		}
		return s
	}
	plain := lipgloss.NewStyle()
	sep := bg(plain).Render(colSep)

	gutter := bg(plain).Render("  ")
	if selected {
		gutter = selectedGutter.Render("▌") + bg(plain).Render(" ")
	}

	// Location replaces the old agent/diff trailer. Left-truncate so the worktree-dir
	// tail (the identifying part) survives a narrow column.
	var locCell string
	if budget := inner - wtPrefixW; budget >= 1 && w.Location != "" {
		locCell = bg(locationStyle).Render(truncLeftWidth(w.Location, budget))
	}

	row := gutter +
		bg(statusStyleFor(w)).Render(statusRaw) + sep +
		bg(ageStyle).Render(ageRaw) + sep +
		bg(textStyle).Render(nameRaw) + sep +
		bg(branchStyle).Render(branchRaw) + sep +
		locCell

	if selected {
		if pad := inner - lipgloss.Width(row); pad > 0 {
			row += bg(plain).Render(strings.Repeat(" ", pad))
		}
	}
	return row
}

// schemaLine renders the column-label row aligned to the worktree grid: a gutter then
// `status`, `worktree`, `branch`, `location` at the offsets worktreeLine uses. Muted,
// lowercase (DESIGN §8), no hue. Clamped to inner like every header line.
func schemaLine(inner int) string {
	row := strings.Repeat(" ", colGutterW) +
		padCell("status", colStatusW) + colSep +
		padCell("age", colAgeW) + colSep +
		padCell("worktree", colNameW) + colSep +
		padCell("branch", colBranchW) + colSep +
		"location"
	return clampWidth(mutedStyle.Render(row), inner)
}

// hasWorktree reports whether any session has at least one worktree row to label.
func (m *DashboardModel) hasWorktree() bool {
	for si := range m.views {
		if len(m.views[si].Worktrees) > 0 {
			return true
		}
	}
	return false
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
	if badge := caffeinateBadge(sv.Caffeinate); badge != "" {
		head += bg(plain).Render(" ") + bg(caffeinateStyle).Render(badge)
	}

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

// previewBox renders the side preview as its own bordered box: a header (worktree · status
// · age, plus a muted "quiet" tag) and the captured pane tail, clamped to the box height.
func (m *DashboardModel) previewBox(width, height int) string {
	innerW := width - 4
	if innerW < 1 {
		innerW = 1
	}
	head := m.previewLabel
	if w := m.selected(); w != nil {
		head += "  " + mutedStyle.Render(w.Status.Label())
		if w.AgeLabel != "" {
			head += " " + mutedStyle.Render(w.AgeLabel)
		}
		if w.Quiet {
			head += " " + mutedStyle.Render("quiet")
		}
	}
	rows := []string{clampWidth(titleStyle.Render(head), innerW), clampWidth(mutedStyle.Render(strings.Repeat("─", innerW)), innerW)}
	body := m.previewLines
	if len(body) == 0 {
		body = []string{mutedStyle.Render("(no output)")}
	}
	if max := (height - 2) - len(rows); max >= 1 && len(body) > max {
		body = body[len(body)-max:] // keep the freshest tail
	}
	for _, ln := range body {
		rows = append(rows, clampWidth(truncateWidth(ln, innerW), innerW))
	}
	return panelStyle.Width(width - 2).Height(height - 2).Render(strings.Join(rows, "\n"))
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

// truncateWidth shortens s to at most max DISPLAY columns (not runes), adding an
// ellipsis when it must cut — the wide-rune-aware sibling of truncate, so a CJK or emoji
// string occupies the columns it actually paints.
func truncateWidth(s string, max int) string {
	if max < 1 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := lipgloss.Width(string(r))
		if w+rw > max-1 { // reserve one column for the ellipsis
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return b.String() + "…"
}

// truncLeftWidth shortens s to at most max display columns by dropping from the LEFT,
// prefixing "…" so the trailing (most specific) portion survives — the path-tail case,
// the mirror of truncateWidth which cuts from the right.
func truncLeftWidth(s string, max int) string {
	if max < 1 {
		return ""
	}
	if lipgloss.Width(s) <= max {
		return s
	}
	if max == 1 {
		return "…"
	}
	runes := []rune(s)
	w := 0
	i := len(runes)
	for i > 0 {
		rw := lipgloss.Width(string(runes[i-1]))
		if w+rw > max-1 { // reserve one column for the leading ellipsis
			break
		}
		w += rw
		i--
	}
	return "…" + string(runes[i:])
}

// padCell truncates s to width display columns and pads it to exactly that many columns,
// measured by display width — a byte-based %-Ns over-pads CJK/emoji and shoves the later
// columns right, breaking the status-first grid (DESIGN §5.1).
func padCell(s string, width int) string {
	s = truncateWidth(s, width)
	if pad := width - lipgloss.Width(s); pad > 0 {
		s += strings.Repeat(" ", pad)
	}
	return s
}

// refresh re-reads the view-model after a child action, recording any error as a
// transient notice. It never quits the dashboard. When the action ran in the background,
// output is the child's combined output — preferred for the notice because it carries
// eme's friendly message (summary line) rather than a bare "exit status 1".
func (m *DashboardModel) refresh(actionErr error, output string) {
	switch {
	case actionErr == nil:
		m.notice = ""
	case strings.TrimSpace(output) != "":
		m.notice = firstMeaningfulLine(output)
	default:
		m.notice = actionErr.Error()
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
	m.refreshPreview()
}

// SetStatusReload installs the cheap status-only reload the auto-refresh ticker uses
// (raw state + snapshot, no git diff / reconcile). Separate from the full post-action
// reload so ticks stay cheap.
func (m *DashboardModel) SetStatusReload(fn func() ([]SessionView, error)) {
	m.statusReload = fn
}

// SetPreview installs the read-only full-pane capture the side preview uses.
func (m *DashboardModel) SetPreview(fn func(sessionID, worktreeName string) ([]string, error)) {
	m.previewCapture = fn
}

// togglePreview opens the side preview for the selected worktree or closes it. It refuses
// below previewMinWidth (the split would crush both panes) with an explaining notice.
func (m *DashboardModel) togglePreview() {
	if m.preview {
		m.closePreview()
		return
	}
	if m.width > 0 && m.width < previewMinWidth {
		m.notice = "terminal too narrow for the side preview — widen the popup"
		return
	}
	if m.selected() == nil {
		m.notice = "select a worktree to preview (the cursor is on a session)"
		return
	}
	m.preview = true
	m.refreshPreview()
}

// refreshPreview re-captures the selected pane into the panel; it closes the preview when
// the selection is gone, and keeps the last lines on a transient capture error (F1).
func (m *DashboardModel) refreshPreview() {
	if !m.preview {
		return
	}
	w := m.selected()
	if w == nil {
		m.closePreview()
		return
	}
	m.previewLabel, m.previewSID = w.Name, w.SessionID
	if m.previewCapture == nil {
		return
	}
	if lines, err := m.previewCapture(w.SessionID, w.Name); err == nil {
		m.previewLines = lines
	}
}

// closePreview tears the panel down so the tree reclaims the full width.
func (m *DashboardModel) closePreview() {
	m.preview = false
	m.previewLines = nil
	m.previewLabel = ""
	m.previewSID = ""
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
	m.refreshPreview()
}

// restoreCursorByIdentity points the cursor at the row matching the given identity (a
// session header by wantSession, or a worktree by wantSID+wantName); if none matches, the
// clamped index from rebuildRows stands. The side preview follows the cursor via the
// refreshPreview the reload callers run after this.
func (m *DashboardModel) restoreCursorByIdentity(wantSession, wantSID, wantName string) {
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

// toggleSortMode flips attention-first sort, rebuilds, and keeps the cursor on the same
// worktree, with a transient confirmation in the notice line.
func (m *DashboardModel) toggleSortMode() {
	var wantSession, wantSID, wantName string
	if r := m.currentRow(); r != nil {
		if r.kind == rowSession {
			wantSession = sessionKey(m.views[r.session])
		} else {
			w := m.views[r.session].Worktrees[r.worktree]
			wantSID, wantName = w.SessionID, w.Name
		}
	}
	m.sortByAttention = !m.sortByAttention
	m.rebuildRows()
	m.restoreCursorByIdentity(wantSession, wantSID, wantName)
	if m.sortByAttention {
		m.notice = "sort: attention-first (waiting/crashed up)"
	} else {
		m.notice = "sort: default order"
	}
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
	m.restoreCursorByIdentity(wantSession, wantSID, wantName)
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

// confirmArgs maps a staged confirm plus the pressed key to the child `eme` args to
// run, or ok=false to cancel. It is the pure decision behind the confirm prompt, split
// out so the EXACT argv (not merely "a command ran") is unit-testable — a regression
// that swapped forget for kill on the keep-files key would otherwise pass silently.
func confirmArgs(t *killTarget, key string) ([]string, bool) {
	switch {
	case t.escalated:
		// Second-stage project confirm: history is on no remote. D forces the delete;
		// f still forgets (keeps files). The affirmative is D, not y, so a reflexive y
		// from the first prompt does not blow past the louder warning.
		switch key {
		case "D":
			return []string{"kill", t.sessionID, "--force-unpushed"}, true
		case "f":
			return []string{"forget", t.sessionID}, true
		}
	case t.isMain:
		// Project: y deletes the files, f forgets it. Anything else cancels.
		switch key {
		case "y":
			return []string{"kill", t.sessionID, "--force"}, true
		case "f":
			return []string{"forget", t.sessionID}, true
		}
	default:
		// Worktree: y confirms removal.
		if key == "y" {
			return []string{"kill", t.sessionID, t.worktreeName, "--force"}, true
		}
	}
	return nil, false
}

// isUnpushedExit reports whether a child `eme` process exited with the unpushed-history
// refusal code. It reads ExitCode() structurally (via the interface *exec.ExitError
// satisfies) so the dashboard need not parse stderr to recognize the guard firing.
func isUnpushedExit(err error) bool {
	if ec, ok := err.(interface{ ExitCode() int }); ok {
		return ec.ExitCode() == errors.ExitUnpushedHistory
	}
	return false
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

// runChildBackground runs `eme <args...>` to completion WITHOUT handing it the terminal, so
// the dashboard keeps rendering uninterrupted (no alt-screen flash). Use it for the work
// behind an in-dashboard modal and for non-interactive actions; its combined output is
// captured and surfaced as a notice on failure. Contrast runChild, which suspends the
// dashboard and gives the child the screen for its own interactive UI.
func (m *DashboardModel) runChildBackground(args ...string) tea.Cmd {
	binary, err := os.Executable()
	if err != nil {
		return func() tea.Msg { return actionFinishedMsg{err: fmt.Errorf("locate eme binary: %w", err)} }
	}
	return func() tea.Msg {
		out, err := exec.Command(binary, args...).CombinedOutput()
		return actionFinishedMsg{err: err, output: string(out)}
	}
}

// firstMeaningfulLine returns the first non-blank line of s with any "eme: " prefix
// stripped — turning a child's multi-line error block into a one-line dashboard notice.
func firstMeaningfulLine(s string) string {
	for _, ln := range strings.Split(s, "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			return strings.TrimPrefix(ln, "eme: ")
		}
	}
	return ""
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
