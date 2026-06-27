package tui

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	emeerrors "github.com/alderwork/eme/internal/errors"
)

// names returns a slice of worktree names in the order given by idx, for test diagnostics.
func names(wts []WorktreeView, idx []int) []string {
	out := make([]string, len(idx))
	for i, wi := range idx {
		out[i] = wts[wi].Name
	}
	return out
}

// manyViews builds sessions x perSession worktrees with globally-unique names, for the
// overflow/viewport tests.
func manyViews(sessions, perSession int) []SessionView {
	var vs []SessionView
	for s := range sessions {
		sid := fmt.Sprintf("proj%d", s)
		var wts []WorktreeView
		for w := range perSession {
			wts = append(wts, WorktreeView{
				Name: fmt.Sprintf("%s-wt%d", sid, w), Branch: fmt.Sprintf("feat/%d", w),
				SessionID: sid, IsMain: w == 0, Status: StatusIdle,
			})
		}
		vs = append(vs, SessionView{DisplayName: sid, Root: "/code/" + sid, Worktrees: wts})
	}
	return vs
}

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

// Deleting a project stages a confirm offering two outcomes: y deletes the files,
// f forgets it (keeps files). y confirms here; TestDashboardKillContext_MainForget
// covers f, and TestDashboardKillContext_MainCancel covers dismissal. The destructive
// action stays on y so a stray double-d (the old muscle memory) still cancels.
func TestDashboardKillContext_MainKillsSession(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 1 // myapp/main (IsMain)
	m.Update(runeKey('d'))
	if m.pending == nil || !m.pending.isMain || m.pending.sessionID != "myapp" {
		t.Fatalf("pending = %+v, want isMain session kill of myapp", m.pending)
	}
	_, cmd := m.Update(runeKey('y')) // y = delete files
	if cmd == nil {
		t.Error("confirming delete should return a command")
	}
	if m.pending != nil {
		t.Error("pending should clear after confirm")
	}
}

// A stray double-d must NOT delete: after staging with d, a second d cancels.
func TestDashboardKillContext_DoubleDCancels(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 1
	m.Update(runeKey('d'))           // stage
	_, cmd := m.Update(runeKey('d')) // second d = cancel, not delete
	if cmd != nil || m.pending != nil {
		t.Error("double-d must cancel, not delete")
	}
}

// f on a staged project confirm forgets it (removes from eme, keeps files on disk)
// rather than deleting — the disk-safe outcome surfaced in the UI.
func TestDashboardKillContext_MainForget(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 1 // myapp/main (IsMain)
	m.Update(runeKey('d'))
	_, cmd := m.Update(runeKey('f'))
	if cmd == nil {
		t.Error("forget should return a command")
	}
	if m.pending != nil {
		t.Error("pending should clear after forget")
	}
}

// Any key other than d/f cancels a staged project confirm without acting.
func TestDashboardKillContext_MainCancel(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 1
	m.Update(runeKey('d'))
	_, cmd := m.Update(runeKey('n'))
	if cmd != nil || m.pending != nil {
		t.Error("cancel should clear pending and return no command")
	}
}

// confirmArgs is the pure decision behind every confirm prompt; assert the EXACT argv
// so a regression that, say, swapped forget for kill on the keep-files key can't slip
// through a "cmd != nil" check.
func TestDashboardConfirmArgs(t *testing.T) {
	proj := &killTarget{sessionID: "app", isMain: true, label: "project app"}
	wt := &killTarget{sessionID: "app", worktreeName: "feat", label: "worktree feat"}
	esc := &killTarget{sessionID: "app", isMain: true, escalated: true, label: "project app"}
	cases := []struct {
		name string
		t    *killTarget
		key  string
		want string // space-joined argv; "" means ok=false (cancel)
	}{
		{"project y deletes files", proj, "y", "kill app --force"},
		{"project f forgets", proj, "f", "forget app"},
		{"project D is inert before escalation", proj, "D", ""},
		{"project other cancels", proj, "n", ""},
		{"worktree y kills", wt, "y", "kill app feat --force"},
		{"worktree f cancels", wt, "f", ""},
		{"escalated D forces past the guard", esc, "D", "kill app --force-unpushed"},
		{"escalated f still forgets", esc, "f", "forget app"},
		{"escalated y does NOT blow through", esc, "y", ""},
	}
	for _, c := range cases {
		args, ok := confirmArgs(c.t, c.key)
		got := ""
		if ok {
			got = strings.Join(args, " ")
		}
		if got != c.want {
			t.Errorf("%s: confirmArgs(%q) = (%v, %v) → %q, want %q", c.name, c.key, args, ok, got, c.want)
		}
	}
}

// fakeExit is an error carrying an ExitCode, mimicking *exec.ExitError so the escalation
// path can be driven without spawning a real child.
type fakeExit int

func (f fakeExit) Error() string { return "exit" }
func (f fakeExit) ExitCode() int { return int(f) }

// A plain project delete refused for unpushed history escalates to a second confirm
// whose D runs --force-unpushed — the only in-UI path to the override — and clears the
// pending tracker once consumed.
func TestDashboardEscalatesOnUnpushedExit(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 1 // myapp/main (IsMain)
	m.Update(runeKey('d'))
	m.Update(runeKey('y')) // dispatch the plain delete; records lastDelete
	if m.lastDelete == nil {
		t.Fatal("y on a project should record lastDelete for possible escalation")
	}
	m.Update(actionFinishedMsg{err: fakeExit(emeerrors.ExitUnpushedHistory)})
	if m.pending == nil || !m.pending.escalated || m.pending.sessionID != "myapp" {
		t.Fatalf("pending = %+v, want an escalated confirm for myapp", m.pending)
	}
	if m.lastDelete != nil {
		t.Error("lastDelete should clear once consumed")
	}
	_, cmd := m.Update(runeKey('D'))
	if cmd == nil {
		t.Error("D on the escalated confirm should run the force-unpushed delete")
	}
	if m.pending != nil {
		t.Error("pending should clear after the escalated choice")
	}
}

// A generic child failure (not the unpushed code) surfaces as a notice, never an
// escalated confirm — escalation is reserved for the guard firing.
func TestDashboardGenericFailureDoesNotEscalate(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 1
	m.Update(runeKey('d'))
	m.Update(runeKey('y'))
	m.Update(actionFinishedMsg{err: fakeExit(1)})
	if m.pending != nil {
		t.Errorf("a generic failure must not escalate, pending = %+v", m.pending)
	}
	if m.notice != "exit" {
		t.Errorf("notice = %q, want the failure surfaced", m.notice)
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

// TestDashboardCreateWorktreeGatedOnPlain: c spawns the create-worktree child on a
// git-backed session, but on a plain (non-git) folder it is gated in the UI — no
// child runs and a one-line notice explains why, instead of failing at runtime.
func TestDashboardCreateWorktreeGatedOnPlain(t *testing.T) {
	views := []SessionView{
		{DisplayName: "repo", Root: "/code/repo", Worktrees: []WorktreeView{
			{Name: "main", Branch: "main", SessionID: "repo", IsMain: true, Status: StatusIdle},
		}},
		{DisplayName: "docs", Root: "/notes/docs", IsPlain: true, Worktrees: []WorktreeView{
			{Name: "main", SessionID: "docs", IsMain: true, Status: StatusIdle},
		}},
	}
	// rows: 0 header repo, 1 repo/main, 2 header docs, 3 docs/main
	m := NewDashboard(views, nil)

	m.cursor = 1 // repo/main → git-backed
	_, cmd := m.Update(runeKey('c'))
	if cmd == nil {
		t.Error("c on a git session should run the create-worktree child")
	}
	if m.notice != "" {
		t.Errorf("notice = %q, want empty on a git session", m.notice)
	}

	m.cursor = 3 // docs/main → plain
	_, cmd = m.Update(runeKey('c'))
	if cmd != nil {
		t.Error("c on a plain folder must not run a child")
	}
	if !strings.Contains(m.notice, "plain folder") {
		t.Errorf("notice = %q, want a plain-folder explanation", m.notice)
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
	m.refresh(nil, "")
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
	m.refresh(nil, "")
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

// TestDashboardPreviewToggleKey: `p` opens the side preview for the selected worktree
// and renders the captured lines; `p` again closes it.
func TestDashboardPreviewToggleKey(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m.cursor = 2 // myapp/feat
	var gotID, gotName string
	m.SetPreview(func(id, name string) ([]string, error) {
		gotID, gotName = id, name
		return []string{"building...", "done"}, nil
	})

	m.Update(runeKey('p'))
	if !m.preview {
		t.Fatal("p should open the side preview")
	}
	if gotID != "myapp" || gotName != "feat" {
		t.Errorf("preview targeted %s/%s, want myapp/feat", gotID, gotName)
	}
	if !strings.Contains(m.View(), "done") {
		t.Error("View should show the previewed lines while open")
	}

	m.Update(runeKey('p'))
	if m.preview {
		t.Error("second p should close the preview")
	}
	if strings.Contains(m.View(), "done") {
		t.Error("closed preview must spend no space")
	}
}

// TestDashboardPreviewClosesOnMoveToHeader: the preview follows the cursor, so moving onto
// a session header (which has no pane) tears it down.
func TestDashboardPreviewClosesOnMoveToHeader(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m.cursor = 1 // myapp/main, a worktree row
	m.SetPreview(func(id, name string) ([]string, error) { return []string{"x"}, nil })
	m.Update(runeKey('p'))
	if !m.preview {
		t.Fatal("precondition: preview open")
	}
	m.Update(runeKey('k')) // up onto the myapp header
	if m.preview {
		t.Error("moving onto a session header should close the preview")
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

// TestPadCellMeasuresDisplayWidth: name/branch cells are padded to an exact DISPLAY
// width (not byte length), so a CJK/emoji name keeps the status-first column grid
// aligned instead of shoving later columns right (#3).
func TestPadCellMeasuresDisplayWidth(t *testing.T) {
	cases := []struct {
		in    string
		width int
	}{
		{"main", 14},
		{"한글워크트리", 14},      // 6 wide runes = 12 cols, fits, padded to 14
		{"日本語のとても長い名前", 10}, // 22 cols, must truncate to fit 10
		{"plain-ascii-too-long-to-fit", 16},
		{"", 8},
	}
	for _, c := range cases {
		if w := lipgloss.Width(padCell(c.in, c.width)); w != c.width {
			t.Errorf("padCell(%q, %d) display width = %d, want %d", c.in, c.width, w, c.width)
		}
	}
}

// TestDashboardPerRowActionsNoticeOnHeader: the per-worktree actions advertised in the
// help (p/a/A/x) explain themselves when the cursor is on a session header rather than
// silently doing nothing (#8).
func TestDashboardPerRowActionsNoticeOnHeader(t *testing.T) {
	for _, key := range []rune{'p', 'a', 'A', 'x'} {
		m := NewDashboard(sampleViews(), nil)
		m.cursor = 0 // myapp header
		m.SetPreview(func(string, string) ([]string, error) { return []string{"x"}, nil })
		m.Update(runeKey(key))
		if m.notice == "" {
			t.Errorf("%q on a session header should set an explanatory notice", key)
		}
		if m.preview {
			t.Errorf("%q on a header should not open the preview", key)
		}
	}
}

// TestDashboardCleanNoticeOnLivePane: x on a running/idle worktree explains the gate
// instead of doing nothing (#8).
func TestDashboardCleanNoticeOnLivePane(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 1 // myapp/main, StatusWorking
	_, cmd := m.Update(runeKey('x'))
	if cmd != nil {
		t.Error("x on a live pane must not run a child")
	}
	if m.notice == "" {
		t.Error("x on a live pane should explain the gate")
	}
}

func TestDashboardRefreshActionErrorIsTransient(t *testing.T) {
	m := NewDashboard(sampleViews(), func() ([]SessionView, error) { return sampleViews(), nil })
	m.refresh(errors.New("kill failed"), "")
	if m.notice != "kill failed" {
		t.Errorf("notice = %q, want the action error", m.notice)
	}
	if len(m.rows) != 5 {
		t.Errorf("rows = %d, want list preserved", len(m.rows))
	}
}

func TestDashboardViewContainsMotifAndStatus(t *testing.T) {
	v := NewDashboard(sampleViews(), nil).View()
	for _, want := range []string{"eme", "myapp", "running", "crashed", "idle", "◜", "✗"} {
		if !strings.Contains(v, want) {
			t.Errorf("View() missing %q\n---\n%s", want, v)
		}
	}
	// One crashed worktree, zero waiting → the tally reads "1 crashed" (danger), not a
	// merged amber "needs you" (DESIGN §5.2).
	if !strings.Contains(v, "1 crashed") {
		t.Errorf("View() should show '1 crashed'\n%s", v)
	}
	if strings.Contains(v, "needs you") {
		t.Errorf("View() must not use the merged 'needs you' tally\n%s", v)
	}
	// The dashboard is wrapped in a rounded-border panel.
	if !strings.Contains(v, "╭") || !strings.Contains(v, "╰") {
		t.Errorf("View() should be wrapped in a rounded-border panel\n%s", v)
	}
}

// TestDashboardHeaderTallySplitsWaitingAndCrashed: the header counter is two
// hue-correct segments — the waiting count and the crashed count — never a single
// merged "needs you" number (DESIGN §5.2: "N waiting" then " · M crashed").
func TestDashboardHeaderTallySplitsWaitingAndCrashed(t *testing.T) {
	views := []SessionView{
		{DisplayName: "app", Root: "/app", Worktrees: []WorktreeView{
			{Name: "a", SessionID: "app", Status: StatusWaiting},
			{Name: "b", SessionID: "app", Status: StatusCrashed},
			{Name: "c", SessionID: "app", Status: StatusWorking},
		}},
	}
	v := NewDashboard(views, nil).View()
	if !strings.Contains(v, "1 waiting") {
		t.Errorf("header should show '1 waiting'\n%s", v)
	}
	if !strings.Contains(v, "1 crashed") {
		t.Errorf("header should show '1 crashed'\n%s", v)
	}
	if strings.Contains(v, "needs you") {
		t.Errorf("header must not use the merged 'needs you' tally\n%s", v)
	}
}

// TestDashboardHeaderNoBeaconWhenNothingWaits locks the reserved-beacon rule: with
// crashes but zero waiting, the one amber (beacon) hue must appear NOWHERE — the crash
// tally is danger, not amber (DESIGN §2 principle 2, §5.2).
func TestDashboardHeaderNoBeaconWhenNothingWaits(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	defer lipgloss.SetColorProfile(termenv.Ascii)

	views := []SessionView{{DisplayName: "app", Root: "/app", Worktrees: []WorktreeView{
		{Name: "b", SessionID: "app", Status: StatusCrashed},
	}}}
	v := NewDashboard(views, nil).View()
	// beacon amber (dark) is #E69F00 = rgb(230,159,0); danger is #D55E00 = rgb(213,94,0).
	if strings.Contains(v, "38;2;230;159;0") {
		t.Errorf("beacon amber must not appear when nothing is waiting (crash counts in danger)\n%s", v)
	}
	if !strings.Contains(v, "38;2;213;94;0") {
		t.Errorf("a crash should render in danger vermillion\n%s", v)
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

// TestDashboardViewFitsPopupHeight is the core overflow guard: with far more rows than
// fit, the rendered panel never grows past the popup — the closing border and the help
// footer stay on-screen (DESIGN §5.5), and a "more" affordance shows the tree scrolled.
func TestDashboardViewFitsPopupHeight(t *testing.T) {
	m := NewDashboard(manyViews(8, 4), nil) // ~47 body lines
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	v := m.View()
	if h := lipgloss.Height(v); h > 20 {
		t.Errorf("View height = %d, want <= 20 (must not overflow the popup)\n%s", h, v)
	}
	if !strings.Contains(v, "╰") {
		t.Errorf("closing border must stay on screen\n%s", v)
	}
	if !strings.Contains(v, "quit") {
		t.Errorf("help footer must stay on screen\n%s", v)
	}
	if !strings.Contains(v, "more") {
		t.Errorf("an overflowing tree should show a 'more' affordance\n%s", v)
	}
}

// TestDashboardViewNoLineWiderThanPopup: no rendered line exceeds the popup width — a
// long name/branch/location is truncated to its column, never wrapped to a second
// row that would push the bottom border off-screen (#7, #2).
func TestDashboardViewNoLineWiderThanPopup(t *testing.T) {
	views := []SessionView{{DisplayName: "proj", Root: "/code/proj", Worktrees: []WorktreeView{
		{Name: "a-very-long-worktree-name-that-overflows", Branch: "feature/a-very-long-branch-name-too",
			SessionID: "proj", IsMain: true, Status: StatusWorking, AgentLabel: "claude-sonnet-with-a-long-suffix",
			Location: "…/some/really/long/path/that/keeps/going/and/going/overflow"},
	}}}
	m := NewDashboard(views, nil)
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 20})
	v := m.View()
	for _, ln := range strings.Split(v, "\n") {
		if w := lipgloss.Width(ln); w > 60 {
			t.Errorf("line width %d exceeds popup width 60: %q", w, ln)
		}
	}
	if h := lipgloss.Height(v); h > 20 {
		t.Errorf("height %d > 20: a long row must not wrap and push the border off", h)
	}

	// TestDashboardViewNoLineWiderThanPopup_PreviewOn: verify the invariant also holds
	// when the side preview is open at a width >= previewMinWidth (72).
	m2 := NewDashboard(views, nil)
	m2.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m2.cursor = 1 // position on the worktree row
	m2.SetPreview(func(sid, name string) ([]string, error) {
		return []string{
			"preview-line-very-long-that-could-overflow-if-not-truncated-correctly",
			"line-2",
		}, nil
	})
	m2.Update(runeKey('p')) // toggle preview on
	if !m2.preview {
		t.Fatal("preview should open at width 100 (>= previewMinWidth)")
	}
	v2 := m2.View()
	for _, ln := range strings.Split(v2, "\n") {
		if w := lipgloss.Width(ln); w > 100 {
			t.Errorf("line width %d exceeds popup width 100 with preview ON: %q", w, ln)
		}
	}
	if h := lipgloss.Height(v2); h > 20 {
		t.Errorf("height %d > 20 with preview ON: preview must not overflow", h)
	}
}

// TestDashboardViewKeepsCursorRowVisible: when the cursor sits past the visible window,
// the windowed tree still includes the cursor's row — the viewport follows the cursor.
func TestDashboardViewKeepsCursorRowVisible(t *testing.T) {
	m := NewDashboard(manyViews(8, 4), nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	m.cursor = len(m.rows) - 1 // last row
	last := m.rows[m.cursor]
	wantName := m.views[last.session].Worktrees[last.worktree].Name
	v := m.View()
	if !strings.Contains(v, wantName) {
		t.Errorf("cursor row %q should be visible in the window\n%s", wantName, v)
	}
	if h := lipgloss.Height(v); h > 16 {
		t.Errorf("height %d > 16", h)
	}
}

// TestWindowBodyKeepsCursorVisibleAtSmallCapacity: when the body window collapses to 1–2
// rows (a short popup with a confirm prompt up), windowBody must still keep the cursor's row on
// screen. At that size no "↑/↓ N more" marker can fit, so the marker-negotiation loop finds
// no arrangement; the fix falls back to a marker-less window centered on the cursor instead
// of anchoring at the top of the list (which silently scrolled the selected row off-screen).
func TestWindowBodyKeepsCursorVisibleAtSmallCapacity(t *testing.T) {
	body := make([]string, 20)
	for i := range body {
		body[i] = fmt.Sprintf("row%d", i)
	}
	for _, capacity := range []int{1, 2, 3} {
		for _, cursor := range []int{0, 7, 19} {
			out := windowBody(body, cursor, capacity)
			if len(out) > capacity {
				t.Errorf("cap=%d cursor=%d: len(out)=%d exceeds capacity", capacity, cursor, len(out))
			}
			found := false
			for _, l := range out {
				if strings.Contains(l, body[cursor]) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("cap=%d cursor=%d: cursor row %q missing from window %v", capacity, cursor, body[cursor], out)
			}
		}
	}
}

// TestDashboardViewExpandedHelpDoesNotOverflow: pressing ? shows the long help without
// pushing the border off-screen on a typical-width popup (#4).
func TestDashboardViewExpandedHelpDoesNotOverflow(t *testing.T) {
	m := NewDashboard(manyViews(6, 4), nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m.Update(runeKey('?'))
	v := m.View()
	if h := lipgloss.Height(v); h > 20 {
		t.Errorf("expanded help pushed height to %d > 20\n%s", h, v)
	}
	if !strings.Contains(v, "preview") { // an expanded-help-only key
		t.Errorf("expanded help should be visible\n%s", v)
	}
	if !strings.Contains(v, "╰") {
		t.Errorf("closing border must stay on screen with expanded help\n%s", v)
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

func TestWorktreeLineShowsLocationNotAgent(t *testing.T) {
	m := NewDashboard(nil, nil)
	w := WorktreeView{Name: "gege", Branch: "gege", Status: StatusWorking, AgentLabel: "claude", Location: "…/eme/gege"}
	line := m.worktreeLine(w, false, 80)
	if !strings.Contains(line, "…/eme/gege") {
		t.Errorf("worktree row should show the location, got %q", line)
	}
	if strings.Contains(line, "claude") {
		t.Errorf("agent label must no longer render in the row, got %q", line)
	}
}

func TestTruncLeftWidth(t *testing.T) {
	cases := []struct {
		s    string
		max  int
		want string
	}{
		{"…/eme/gege", 80, "…/eme/gege"}, // fits → unchanged
		{"abc", 3, "abc"},                // exactly fits
		{"abcdefghij", 5, "…ghij"},       // keep rightmost 4, prefix …
		{"abc", 1, "…"},
		{"abc", 0, ""},
	}
	for _, c := range cases {
		if got := truncLeftWidth(c.s, c.max); got != c.want {
			t.Errorf("truncLeftWidth(%q, %d) = %q, want %q", c.s, c.max, got, c.want)
		}
	}
}

func TestSchemaLine(t *testing.T) {
	s := schemaLine(80)
	for _, label := range []string{"status", "worktree", "branch", "location"} {
		if !strings.Contains(s, label) {
			t.Errorf("schemaLine missing label %q: %q", label, s)
		}
	}
	if w := lipgloss.Width(s); w > 80 {
		t.Errorf("schemaLine width %d exceeds inner 80", w)
	}
}

func TestDashboardViewShowsSchemaRowWhenWorktrees(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m.View()
	if !strings.Contains(v, "location") || !strings.Contains(v, "worktree") {
		t.Errorf("expected the schema label row in the view:\n%s", v)
	}
}

func TestDashboardViewOmitsSchemaRowWhenEmpty(t *testing.T) {
	m := NewDashboard([]SessionView{}, nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	v := m.View()
	if strings.Contains(v, "location") {
		t.Errorf("empty dashboard must not show the schema row:\n%s", v)
	}
}

func TestDashboardSchemaRowStaysPinnedWhenScrolled(t *testing.T) {
	m := NewDashboard(manyViews(8, 4), nil)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 16})
	m.cursor = len(m.rows) - 1 // scroll to the bottom
	v := m.View()
	if !strings.Contains(v, "location") {
		t.Errorf("schema row should stay pinned (in the header) when the tree scrolls:\n%s", v)
	}
}

func TestWorktreeLine_ShowsAge(t *testing.T) {
	m := &DashboardModel{width: 100, height: 24}
	w := WorktreeView{Name: "feat", Branch: "feat", Status: StatusWorking, AgeLabel: "12m", Location: "…/p/feat"}
	line := m.worktreeLine(w, false, 90)
	if !strings.Contains(line, "12m") {
		t.Errorf("worktree row missing age cell: %q", line)
	}
}

func TestSchemaLine_HasAgeLabel(t *testing.T) {
	s := schemaLine(90)
	for _, col := range []string{"status", "age", "worktree", "branch", "location"} {
		if !strings.Contains(s, col) {
			t.Errorf("schema row missing %q: %q", col, s)
		}
	}
}

func TestStatusStyleFor_DimsQuiet(t *testing.T) {
	normal := statusStyleFor(WorktreeView{Status: StatusWorking})
	quiet := statusStyleFor(WorktreeView{Status: StatusWorking, Quiet: true})
	if normal.GetForeground() == quiet.GetForeground() && normal.GetFaint() == quiet.GetFaint() {
		t.Error("quiet working row should render visually distinct (dim) from a busy one")
	}
}

func TestQuiet_DoesNotCountAsAttention(t *testing.T) {
	// Quiet is a soft hint, not a beacon: a quiet WORKING agent must not flip NeedsAttention
	// (which drives the header tally and the ambient ✗/● segment).
	if StatusWorking.NeedsAttention() {
		t.Error("working must never need attention; quiet is layered on working, not a status")
	}
}

func TestAttentionRank(t *testing.T) {
	order := []struct {
		s     AgentStatus
		quiet bool
		rank  int
	}{
		{StatusCrashed, false, 0}, {StatusWaiting, false, 1}, {StatusWorking, true, 2},
		{StatusWorking, false, 3}, {StatusIdle, false, 4}, {StatusExited, false, 5},
	}
	for _, o := range order {
		if got := attentionRank(o.s, o.quiet); got != o.rank {
			t.Errorf("attentionRank(%v,%v) = %d, want %d", o.s, o.quiet, got, o.rank)
		}
	}
}

func TestWorktreeOrder_AttentionFirstWithAgeTiebreak(t *testing.T) {
	t0 := time.Unix(1_750_000_000, 0)
	m := &DashboardModel{views: []SessionView{{Worktrees: []WorktreeView{
		{Name: "idle", Status: StatusIdle},
		{Name: "crash", Status: StatusCrashed},
		{Name: "wait-new", Status: StatusWaiting, StateChangedAt: t0.Add(60 * time.Second)},
		{Name: "wait-old", Status: StatusWaiting, StateChangedAt: t0}, // older → higher
	}}}}
	// default: identity order
	if got := m.worktreeOrder(0); !reflect.DeepEqual(got, []int{0, 1, 2, 3}) {
		t.Fatalf("default order = %v, want [0 1 2 3]", got)
	}
	m.sortByAttention = true
	got := m.worktreeOrder(0)
	want := []int{1, 3, 2, 0} // crash, wait-old (longest), wait-new, idle
	if !reflect.DeepEqual(got, want) {
		t.Errorf("attention order = %v (names %s), want %v", got, names(m.views[0].Worktrees, got), want)
	}
}

func TestTogglePreview_RefusesWhenNarrow(t *testing.T) {
	m := &DashboardModel{width: 60, height: 24, collapsed: map[string]bool{},
		views: []SessionView{{Worktrees: []WorktreeView{{Name: "w", SessionID: "s"}}}}}
	m.rebuildRows()
	m.cursor = 1
	m.previewCapture = func(string, string) ([]string, error) { return []string{"hi"}, nil }
	m.togglePreview()
	if m.preview {
		t.Error("preview must refuse to open below previewMinWidth")
	}
	if m.notice == "" {
		t.Error("a refusal should explain why (notice)")
	}
}

func TestPreview_OpensAndRendersHeader(t *testing.T) {
	m := &DashboardModel{width: 100, height: 24, collapsed: map[string]bool{},
		views: []SessionView{{Worktrees: []WorktreeView{
			{Name: "feat", SessionID: "s", Status: StatusWorking, AgeLabel: "3m"}}}}}
	m.rebuildRows()
	m.cursor = 1
	m.previewCapture = func(sid, name string) ([]string, error) { return []string{"line-A", "line-B"}, nil }
	m.togglePreview()
	if !m.preview || m.previewLabel != "feat" {
		t.Fatalf("preview not opened for feat: open=%v label=%q", m.preview, m.previewLabel)
	}
	out := m.View()
	for _, want := range []string{"feat", "3m", "line-B"} {
		if !strings.Contains(out, want) {
			t.Errorf("preview view missing %q", want)
		}
	}
}

func TestToggleSort_KeepsCursorOnIdentity(t *testing.T) {
	m := &DashboardModel{views: []SessionView{{Worktrees: []WorktreeView{
		{Name: "idle", SessionID: "s", Status: StatusIdle},
		{Name: "crash", SessionID: "s", Status: StatusCrashed},
	}}}, collapsed: map[string]bool{}}
	m.rebuildRows()
	// put the cursor on "idle" (row 1: row 0 is the session header)
	m.cursor = 1
	before := m.selected().Name
	m.toggleSortMode()
	if m.selected() == nil || m.selected().Name != before {
		t.Errorf("cursor jumped off %q after sort toggle", before)
	}
}

func TestNextCaffeinateMode_Cycles(t *testing.T) {
	cases := map[string]string{"": "manual", "manual": "auto", "auto": "off"}
	for in, want := range cases {
		if got := nextCaffeinateMode(in); got != want {
			t.Fatalf("nextCaffeinateMode(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestCaffeinateBadge(t *testing.T) {
	if caffeinateBadge("") != "" {
		t.Fatal("off → no badge")
	}
	if caffeinateBadge("manual") != "(caf)" {
		t.Fatalf("manual badge = %q", caffeinateBadge("manual"))
	}
	if caffeinateBadge("auto") != "(caf~)" {
		t.Fatalf("auto badge = %q", caffeinateBadge("auto"))
	}
}

func TestSessionLine_ShowsCaffeinateBadge(t *testing.T) {
	m := NewDashboard([]SessionView{{
		DisplayName: "proj", Root: "/proj", Caffeinate: "auto",
		Worktrees: []WorktreeView{{Name: "main", SessionID: "proj-1"}},
	}}, func() ([]SessionView, error) { return nil, nil })
	m.width, m.height = 100, 24
	out := m.View()
	if !strings.Contains(out, "(caf~)") {
		t.Fatalf("expected the auto badge in the session line, got:\n%s", out)
	}
}
