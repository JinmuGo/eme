package tui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"
)

func runeKey(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func sampleViews() []SessionView {
	return []SessionView{
		{DisplayName: "myapp", Root: "/code/myapp", Worktrees: []WorktreeView{
			{Name: "main", Branch: "main", SessionID: "myapp", IsMain: true, Status: StatusWorking, AgentLabel: "claude"},
			{Name: "feat", Branch: "feat/x", SessionID: "myapp", Status: StatusCrashed},
		}},
		{DisplayName: "api", Root: "/code/api", Worktrees: []WorktreeView{
			{Name: "main", Branch: "main", SessionID: "api", IsMain: true, Status: StatusIdle},
		}},
	}
}

// sampleViews flattens (all expanded) to:
//
//	row 0  session header  myapp
//	row 1  worktree        myapp/main
//	row 2  worktree        myapp/feat
//	row 3  session header  api
//	row 4  worktree        api/main
func TestDashboardFlattenAndCursorClamp(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	if len(m.rows) != 5 {
		t.Fatalf("rows = %d, want 5 (2 headers + 3 worktrees)", len(m.rows))
	}
	for range 10 {
		m.Update(runeKey('j'))
	}
	if m.cursor != 4 {
		t.Errorf("cursor = %d, want clamped at 4", m.cursor)
	}
}

func TestDashboardKillContext_MainKillsSession(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 1 // myapp/main (IsMain)
	m.Update(runeKey('d'))
	if m.pending == nil || !m.pending.isMain || m.pending.sessionID != "myapp" {
		t.Fatalf("pending = %+v, want isMain session kill of myapp", m.pending)
	}
	_, cmd := m.Update(runeKey('y'))
	if cmd == nil {
		t.Error("confirming kill should return a command")
	}
	if m.pending != nil {
		t.Error("pending should clear after confirm")
	}
}

func TestDashboardKillContext_WorktreeKill(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 2 // myapp/feat (not main)
	m.Update(runeKey('d'))
	if m.pending == nil || m.pending.isMain || m.pending.worktreeName != "feat" {
		t.Fatalf("pending = %+v, want worktree kill of feat", m.pending)
	}
	_, cmd := m.Update(runeKey('n')) // cancel
	if cmd != nil || m.pending != nil {
		t.Error("cancel should clear pending and return no command")
	}
}

// TestDashboardKillContextHeaderKillsSession: d on a session header stages a kill of
// the whole session (isMain), the same as d on its main worktree.
func TestDashboardKillContextHeaderKillsSession(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 0 // myapp header
	m.Update(runeKey('d'))
	if m.pending == nil || !m.pending.isMain || m.pending.sessionID != "myapp" {
		t.Fatalf("pending = %+v, want isMain session kill of myapp", m.pending)
	}
}

func TestDashboardRefreshRebuildsRows(t *testing.T) {
	m := NewDashboard(sampleViews(), func() ([]SessionView, error) {
		return []SessionView{{DisplayName: "api", Root: "/code/api", Worktrees: []WorktreeView{
			{Name: "main", SessionID: "api", IsMain: true, Status: StatusIdle},
		}}}, nil
	})
	m.cursor = 4
	m.refresh(nil)
	// 1 header + 1 worktree.
	if len(m.rows) != 2 {
		t.Fatalf("rows = %d, want 2 after refresh", len(m.rows))
	}
	if m.cursor < 0 || m.cursor >= len(m.rows) {
		t.Errorf("cursor = %d, want clamped into range", m.cursor)
	}
}

// TestDashboardRefreshReloadErrorKeepsLastKnown locks the F1 guardrail: when the
// reload (i.e. the tmux pane snapshot) fails, refresh keeps the last-known views
// verbatim and only records a transient notice — it must never blank the list or
// repaint a guessed status.
func TestDashboardRefreshReloadErrorKeepsLastKnown(t *testing.T) {
	m := NewDashboard(sampleViews(), func() ([]SessionView, error) {
		return nil, errors.New("snapshot read failed")
	})
	rowsBefore := len(m.rows)
	m.refresh(nil)
	if len(m.rows) != rowsBefore {
		t.Errorf("rows = %d, want %d preserved on reload error (F1 guardrail)", len(m.rows), rowsBefore)
	}
	if m.notice != "refresh failed: snapshot read failed" {
		t.Errorf("notice = %q, want the reload error surfaced", m.notice)
	}
}

// TestDashboardStickyCursorAcrossReload locks ARCH-5: when the row set reorders
// under the cursor (a session appears), the selection stays on the SAME worktree by
// identity rather than a fixed index.
func TestDashboardStickyCursorAcrossReload(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 2 // myapp/feat
	if w := m.selected(); w == nil || w.Name != "feat" {
		t.Fatalf("precondition: cursor should be on feat, got %+v", m.selected())
	}

	// A new session appears at the top, pushing myapp/feat from index 2 to index 4.
	reordered := []SessionView{
		{DisplayName: "api", Root: "/code/api", Worktrees: []WorktreeView{
			{Name: "main", Branch: "main", SessionID: "api", IsMain: true, Status: StatusIdle},
		}},
		{DisplayName: "myapp", Root: "/code/myapp", Worktrees: []WorktreeView{
			{Name: "main", Branch: "main", SessionID: "myapp", IsMain: true, Status: StatusWorking},
			{Name: "feat", Branch: "feat/x", SessionID: "myapp", Status: StatusCrashed},
		}},
	}
	m.applyViews(reordered)

	if w := m.selected(); w == nil || w.SessionID != "myapp" || w.Name != "feat" {
		t.Errorf("cursor jumped off feat after reorder; selected = %+v", m.selected())
	}
	if m.cursor != 4 {
		t.Errorf("cursor = %d, want 4 (feat's new index)", m.cursor)
	}
}

// TestDashboardStickyHeaderAcrossReload: a cursor parked on a session header stays on
// that session by identity when the rows reorder.
func TestDashboardStickyHeaderAcrossReload(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 0 // myapp header
	m.applyViews([]SessionView{
		{DisplayName: "api", Root: "/code/api", Worktrees: []WorktreeView{
			{Name: "main", SessionID: "api", IsMain: true, Status: StatusIdle},
		}},
		{DisplayName: "myapp", Root: "/code/myapp", Worktrees: []WorktreeView{
			{Name: "main", SessionID: "myapp", IsMain: true, Status: StatusWorking},
			{Name: "feat", SessionID: "myapp", Status: StatusCrashed},
		}},
	})
	r := m.currentRow()
	if r == nil || r.kind != rowSession {
		t.Fatalf("cursor should still be on a session header, got %+v", r)
	}
	if sessionKey(m.views[r.session]) != "myapp" {
		t.Errorf("cursor jumped off the myapp header after reorder; on %q", sessionKey(m.views[r.session]))
	}
}

// TestDashboardStickyCursorFallsBackWhenSelectionGone: if the selected worktree
// disappears, the cursor falls back to a clamped, valid row (no panic).
func TestDashboardStickyCursorFallsBackWhenSelectionGone(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 2 // myapp/feat

	m.applyViews([]SessionView{
		{DisplayName: "api", Root: "/code/api", Worktrees: []WorktreeView{
			{Name: "main", SessionID: "api", IsMain: true, Status: StatusIdle},
		}},
	})

	if m.cursor < 0 || m.cursor >= len(m.rows) {
		t.Fatalf("cursor %d out of range after selection vanished (rows=%d)", m.cursor, len(m.rows))
	}
	if m.currentRow() == nil {
		t.Error("currentRow() should be valid after fallback, got nil")
	}
}

// TestDashboardTickReloadStatusLiveDiffCarried locks the tick contract: status goes
// live from the cheap reload while the last-known git diff is carried forward, and
// the cursor stays put.
func TestDashboardTickReloadStatusLiveDiffCarried(t *testing.T) {
	initial := []SessionView{
		{DisplayName: "myapp", Root: "/code/myapp", Worktrees: []WorktreeView{
			{Name: "feat", Branch: "feat/x", SessionID: "myapp", Status: StatusIdle,
				Added: 3, Deleted: 1, HasDiff: true},
		}},
	}
	m := NewDashboard(initial, nil)
	m.cursor = 1 // the worktree row (row 0 is the session header)

	// The status-only reload reports the agent now running and (as the cheap path)
	// carries NO diff of its own.
	m.SetStatusReload(func() ([]SessionView, error) {
		return []SessionView{
			{DisplayName: "myapp", Root: "/code/myapp", Worktrees: []WorktreeView{
				{Name: "feat", Branch: "feat/x", SessionID: "myapp", Status: StatusWorking},
			}},
		}, nil
	})
	m.tickReload()

	w := m.selected()
	if w == nil || w.Name != "feat" {
		t.Fatalf("cursor lost feat after tick; selected = %+v", w)
	}
	if w.Status != StatusWorking {
		t.Errorf("status = %v, want StatusWorking (live from tick)", w.Status)
	}
	if !w.HasDiff || w.Added != 3 || w.Deleted != 1 {
		t.Errorf("diff not carried: HasDiff=%v +%d -%d, want +3 -1", w.HasDiff, w.Added, w.Deleted)
	}
}

// TestDashboardTickReloadErrorKeepsLastKnown: a transient status-read failure is
// silent and preserves last-known views (F1).
func TestDashboardTickReloadErrorKeepsLastKnown(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.SetStatusReload(func() ([]SessionView, error) { return nil, errors.New("snapshot read failed") })
	rowsBefore := len(m.rows)
	statusBefore := m.views[0].Worktrees[0].Status

	m.tickReload()

	if len(m.rows) != rowsBefore {
		t.Errorf("rows = %d, want %d preserved on tick error", len(m.rows), rowsBefore)
	}
	if m.views[0].Worktrees[0].Status != statusBefore {
		t.Error("status changed on a failed tick; must keep last-known")
	}
	if m.notice != "" {
		t.Errorf("tick failure should be silent, notice = %q", m.notice)
	}
}

// TestDashboardTickReloadNilIsNoop: with no statusReload installed the tick is inert.
func TestDashboardTickReloadNilIsNoop(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	rowsBefore := len(m.rows)
	m.tickReload() // must not panic
	if len(m.rows) != rowsBefore {
		t.Errorf("rows changed on a no-op tick: %d != %d", len(m.rows), rowsBefore)
	}
}

// --- folding (nvim h/l navigation) -------------------------------------------

// TestDashboardRowsIncludeSessionHeaders: the flattened list leads each session with
// a selectable header row; the cursor can rest on it (selected() is nil there).
func TestDashboardRowsIncludeSessionHeaders(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	if m.rows[0].kind != rowSession || m.rows[0].session != 0 {
		t.Fatalf("row 0 should be the myapp header, got %+v", m.rows[0])
	}
	m.cursor = 0
	if m.selected() != nil {
		t.Error("selected() should be nil on a session header")
	}
	if m.selectedSession() != 0 {
		t.Errorf("selectedSession() = %d, want 0", m.selectedSession())
	}
}

// TestDashboardFoldCollapsesAndHidesWorktrees: h on a session header folds it, hiding
// its worktree rows while the header stays selected.
func TestDashboardFoldCollapsesAndHidesWorktrees(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 0 // myapp header
	m.Update(runeKey('h'))
	// myapp's 2 worktrees are hidden: header(myapp), header(api), api/main = 3 rows.
	if len(m.rows) != 3 {
		t.Fatalf("rows = %d, want 3 after folding myapp", len(m.rows))
	}
	if r := m.currentRow(); r == nil || r.kind != rowSession || r.session != 0 {
		t.Errorf("cursor should stay on the myapp header, got %+v", r)
	}
	if !m.isCollapsed(0) {
		t.Error("myapp should be collapsed")
	}
}

// TestDashboardExpandWithL: l on a folded header expands it again.
func TestDashboardExpandWithL(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 0
	m.Update(runeKey('h')) // fold
	m.Update(runeKey('l')) // expand
	if m.isCollapsed(0) {
		t.Error("l should expand the folded myapp")
	}
	if len(m.rows) != 5 {
		t.Errorf("rows = %d, want 5 after expanding", len(m.rows))
	}
}

// TestDashboardFoldLeftFromWorktreeJumpsToHeader: h on a worktree folds its parent and
// parks the cursor on the header (the row never vanishes beneath the user).
func TestDashboardFoldLeftFromWorktreeJumpsToHeader(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 2 // myapp/feat
	m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	if !m.isCollapsed(0) {
		t.Error("h on a worktree should collapse its parent session")
	}
	if r := m.currentRow(); r == nil || r.kind != rowSession || r.session != 0 {
		t.Errorf("cursor should jump to the myapp header, got %+v", r)
	}
}

// TestDashboardRightStepsIntoFirstChild: l on an expanded header moves into its first
// worktree rather than toggling the fold.
func TestDashboardRightStepsIntoFirstChild(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 0 // myapp header, expanded
	m.Update(tea.KeyMsg{Type: tea.KeyRight})
	if w := m.selected(); w == nil || w.Name != "main" {
		t.Errorf("l on an expanded header should step into myapp/main, got %+v", w)
	}
}

// TestDashboardEnterTogglesFoldOnHeader: Enter on a header folds/unfolds it and does
// NOT record a switch target (that is reserved for worktrees).
func TestDashboardEnterTogglesFoldOnHeader(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 0
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd != nil {
		t.Error("Enter on a header should not return a command")
	}
	if !m.isCollapsed(0) {
		t.Error("Enter on an expanded header should fold it")
	}
	if _, _, ok := m.SwitchTarget(); ok {
		t.Error("folding must not record a switch target")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.isCollapsed(0) {
		t.Error("Enter again should unfold")
	}
}

// TestDashboardFoldStateSurvivesReload: fold state is keyed by session identity, so a
// reload that reorders rows keeps the session folded.
func TestDashboardFoldStateSurvivesReload(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 0
	m.Update(runeKey('h')) // fold myapp
	// api appears first on reload; myapp must remain folded.
	m.applyViews([]SessionView{
		{DisplayName: "api", Root: "/code/api", Worktrees: []WorktreeView{
			{Name: "main", SessionID: "api", IsMain: true, Status: StatusIdle},
		}},
		{DisplayName: "myapp", Root: "/code/myapp", Worktrees: []WorktreeView{
			{Name: "main", SessionID: "myapp", IsMain: true, Status: StatusWorking},
			{Name: "feat", SessionID: "myapp", Status: StatusCrashed},
		}},
	})
	if !m.collapsed["myapp"] {
		t.Error("myapp should remain folded across a reload (keyed by SessionID)")
	}
	// rows: api hdr, api/main, myapp hdr = 3 (myapp's worktrees still hidden).
	if len(m.rows) != 3 {
		t.Errorf("rows = %d, want 3 (myapp still folded after reload)", len(m.rows))
	}
}

// TestDashboardPeekToggle: `p` opens the read-only peek for the selected worktree
// and renders the captured lines; `p` again closes it.
func TestDashboardPeekToggle(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 2 // myapp/feat
	var gotID, gotName string
	m.SetPeek(func(id, name string) ([]string, error) {
		gotID, gotName = id, name
		return []string{"building...", "done"}, nil
	})

	m.Update(runeKey('p'))
	if !m.peeking {
		t.Fatal("p should open the peek")
	}
	if gotID != "myapp" || gotName != "feat" {
		t.Errorf("peek targeted %s/%s, want myapp/feat", gotID, gotName)
	}
	if len(m.peekLines) != 2 || m.peekLines[1] != "done" {
		t.Errorf("peekLines = %v, want the captured lines", m.peekLines)
	}
	if !strings.Contains(m.View(), "done") {
		t.Error("View should show the peeked lines while peeking")
	}

	m.Update(runeKey('p'))
	if m.peeking {
		t.Error("second p should close the peek")
	}
	if strings.Contains(m.View(), "done") {
		t.Error("closed peek must spend zero rows")
	}
}

// TestDashboardPeekClosesOnMove: the peek belongs to the row it was opened on, so
// moving the cursor closes it (never a standing panel; DESIGN §5.7).
func TestDashboardPeekClosesOnMove(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 1 // a worktree row, so the peek can open
	m.SetPeek(func(id, name string) ([]string, error) { return []string{"x"}, nil })
	m.Update(runeKey('p'))
	if !m.peeking {
		t.Fatal("precondition: peek open")
	}
	m.Update(runeKey('j'))
	if m.peeking {
		t.Error("moving down should close the peek")
	}
}

// TestDashboardPeekNilSeamIsNoop: with no peek installed, `p` is inert (no panic).
func TestDashboardPeekNilSeamIsNoop(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 1 // a worktree row: exercise the nil peek seam, not the header no-op
	m.Update(runeKey('p'))
	if m.peeking {
		t.Error("p with no peek seam should stay closed")
	}
}

// TestDashboardPeekErrorSurfacesNotice: a capture failure shows a transient notice
// and leaves the peek closed (never a false panel).
func TestDashboardPeekErrorSurfacesNotice(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 1 // a worktree row, so the peek fn is actually invoked
	m.SetPeek(func(id, name string) ([]string, error) { return nil, errors.New("pane gone") })
	m.Update(runeKey('p'))
	if m.peeking {
		t.Error("peek should stay closed on error")
	}
	if m.notice != "peek failed: pane gone" {
		t.Errorf("notice = %q, want the peek error surfaced", m.notice)
	}
}

// TestDashboardCleanKeyRunsChildForDeadPane: `x` on a crashed worktree dispatches
// the `eme clean` child (which respawns the dead pane and clears the record).
func TestDashboardCleanKeyRunsChildForDeadPane(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 2 // myapp/feat, StatusCrashed
	_, cmd := m.Update(runeKey('x'))
	if cmd == nil {
		t.Error("x on a crashed worktree should run the clean child")
	}
}

// TestDashboardCleanKeyNoopForLivePane: `x` is gated to dead-pane statuses, so it is
// a no-op on a running (or idle) worktree — never disturbing a live agent.
func TestDashboardCleanKeyNoopForLivePane(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 1 // myapp/main, StatusWorking
	_, cmd := m.Update(runeKey('x'))
	if cmd != nil {
		t.Error("x on a running worktree should be a no-op")
	}
}

func TestDashboardRefreshActionErrorIsTransient(t *testing.T) {
	m := NewDashboard(sampleViews(), func() ([]SessionView, error) { return sampleViews(), nil })
	m.refresh(errors.New("kill failed"))
	if m.notice != "kill failed" {
		t.Errorf("notice = %q, want the action error", m.notice)
	}
	if len(m.rows) != 5 {
		t.Errorf("rows = %d, want list preserved", len(m.rows))
	}
}

func TestDashboardViewContainsMotifAndStatus(t *testing.T) {
	v := NewDashboard(sampleViews(), nil).View()
	for _, want := range []string{"eme", "needs you", "myapp", "running", "crashed", "idle", "◐", "✗"} {
		if !strings.Contains(v, want) {
			t.Errorf("View() missing %q\n---\n%s", want, v)
		}
	}
	// One crashed worktree → "1 needs you" (clean exits no longer count).
	if !strings.Contains(v, "1 needs you") {
		t.Errorf("View() should show '1 needs you'\n%s", v)
	}
	// The dashboard is wrapped in a rounded-border panel.
	if !strings.Contains(v, "╭") || !strings.Contains(v, "╰") {
		t.Errorf("View() should be wrapped in a rounded-border panel\n%s", v)
	}
}

// TestDashboardViewMarksFoldedSession: a folded session shows the ▸ caret and a
// hidden-count, and its worktrees no longer render.
func TestDashboardViewMarksFoldedSession(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 0
	m.Update(runeKey('h')) // fold myapp
	v := m.View()
	if !strings.Contains(v, "▸") {
		t.Errorf("folded session should show the ▸ caret\n%s", v)
	}
	if !strings.Contains(v, "hidden") {
		t.Errorf("folded session should show a hidden-count\n%s", v)
	}
	if strings.Contains(v, "feat") {
		t.Errorf("folded session must hide its worktrees (feat)\n%s", v)
	}
}

func TestDashboardEnterRecordsSwitchAndQuits(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	if _, _, ok := m.SwitchTarget(); ok {
		t.Fatal("SwitchTarget should be empty before Enter")
	}
	m.cursor = 2 // myapp/feat

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})

	session, worktree, ok := m.SwitchTarget()
	if !ok || session != "myapp" || worktree != "feat" {
		t.Fatalf("SwitchTarget = (%q,%q,%v), want (myapp,feat,true)", session, worktree, ok)
	}
	if cmd == nil {
		t.Fatal("Enter should return a command")
	}
	// Enter must quit cleanly (so the terminal is restored before the caller
	// execs `eme switch`), not exec from inside a command.
	if _, isQuit := cmd().(tea.QuitMsg); !isQuit {
		t.Error("Enter should return tea.Quit so bubbletea restores the terminal")
	}
}

// TestDashboardLOpensWorktree: l/→ on a worktree opens it (same as Enter), recording
// the switch target and quitting.
func TestDashboardLOpensWorktree(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 2 // myapp/feat
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	session, worktree, ok := m.SwitchTarget()
	if !ok || session != "myapp" || worktree != "feat" {
		t.Fatalf("SwitchTarget = (%q,%q,%v), want (myapp,feat,true)", session, worktree, ok)
	}
	if cmd == nil {
		t.Fatal("l on a worktree should return a command")
	}
}

// TestDashboardSelectedRowIsHighlightBar locks the headline visual: the worktree
// under the cursor renders as a full-width background highlight bar, and other
// rows do not.
func TestDashboardSelectedRowIsHighlightBar(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	m := NewDashboard(sampleViews(), nil)
	w := sampleViews()[0].Worktrees[0]

	selected := m.worktreeLine(w, true, 60)
	if !strings.Contains(selected, "48;2;") {
		t.Errorf("selected row should carry a background escape (highlight bar), got %q", selected)
	}
	if got := lipgloss.Width(selected); got != 60 {
		t.Errorf("selected row width = %d, want 60 (fills the inner width)", got)
	}

	plain := m.worktreeLine(w, false, 60)
	if strings.Contains(plain, "48;2;") {
		t.Errorf("non-selected row should not carry a background escape, got %q", plain)
	}
}

// TestDashboardSelectedHeaderIsHighlightBar: a selected session header also fills the
// inner width with the surface lift, so selection reads the same on headers.
func TestDashboardSelectedHeaderIsHighlightBar(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	m := NewDashboard(sampleViews(), nil)
	selected := m.sessionLine(0, true, 60)
	if !strings.Contains(selected, "48;2;") {
		t.Errorf("selected header should carry a background escape, got %q", selected)
	}
	if got := lipgloss.Width(selected); got != 60 {
		t.Errorf("selected header width = %d, want 60", got)
	}
	plain := m.sessionLine(0, false, 60)
	if strings.Contains(plain, "48;2;") {
		t.Errorf("non-selected header should not carry a background escape, got %q", plain)
	}
}
