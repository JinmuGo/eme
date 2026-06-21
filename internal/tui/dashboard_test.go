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

func TestDashboardFlattenAndCursorClamp(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	if len(m.rows) != 3 {
		t.Fatalf("rows = %d, want 3 (flattened worktrees)", len(m.rows))
	}
	for i := 0; i < 10; i++ {
		m.Update(runeKey('j'))
	}
	if m.cursor != 2 {
		t.Errorf("cursor = %d, want clamped at 2", m.cursor)
	}
}

func TestDashboardKillContext_MainKillsSession(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	m.cursor = 0 // myapp/main (IsMain)
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
	m.cursor = 1 // myapp/feat (not main)
	m.Update(runeKey('d'))
	if m.pending == nil || m.pending.isMain || m.pending.worktreeName != "feat" {
		t.Fatalf("pending = %+v, want worktree kill of feat", m.pending)
	}
	_, cmd := m.Update(runeKey('n')) // cancel
	if cmd != nil || m.pending != nil {
		t.Error("cancel should clear pending and return no command")
	}
}

func TestDashboardRefreshRebuildsRows(t *testing.T) {
	m := NewDashboard(sampleViews(), func() ([]SessionView, error) {
		return []SessionView{{DisplayName: "api", Root: "/code/api", Worktrees: []WorktreeView{
			{Name: "main", SessionID: "api", IsMain: true, Status: StatusIdle},
		}}}, nil
	})
	m.cursor = 2
	m.refresh(nil)
	if len(m.rows) != 1 {
		t.Fatalf("rows = %d, want 1 after refresh", len(m.rows))
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want clamped to 0", m.cursor)
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
	rowsBefore := len(m.rows) // 3 (flattened sampleViews)
	m.refresh(nil)
	if len(m.rows) != rowsBefore {
		t.Errorf("rows = %d, want %d preserved on reload error (F1 guardrail)", len(m.rows), rowsBefore)
	}
	if m.notice != "refresh failed: snapshot read failed" {
		t.Errorf("notice = %q, want the reload error surfaced", m.notice)
	}
}

func TestDashboardRefreshActionErrorIsTransient(t *testing.T) {
	m := NewDashboard(sampleViews(), func() ([]SessionView, error) { return sampleViews(), nil })
	m.refresh(errors.New("kill failed"))
	if m.notice != "kill failed" {
		t.Errorf("notice = %q, want the action error", m.notice)
	}
	if len(m.rows) != 3 {
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

func TestDashboardEnterRecordsSwitchAndQuits(t *testing.T) {
	m := NewDashboard(sampleViews(), nil)
	if _, _, ok := m.SwitchTarget(); ok {
		t.Fatal("SwitchTarget should be empty before Enter")
	}
	m.cursor = 1 // myapp/feat

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
