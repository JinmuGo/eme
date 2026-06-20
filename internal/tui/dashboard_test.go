package tui

import (
	"errors"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/jinmu/eme/internal/state"
)

func runeKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestDashboardRefresh_ReloadsAndClearsNoticeAndClampsCursor(t *testing.T) {
	// Cursor sits on the last of three sessions; a child action then removes two
	// of them. refresh must adopt the new list, clamp the now out-of-range
	// cursor, and clear a stale notice from a previous failed action.
	m := NewDashboard(
		[]state.Session{{ID: "a"}, {ID: "b"}, {ID: "c"}},
		func() ([]state.Session, error) { return []state.Session{{ID: "a"}}, nil },
	)
	m.cursor = 2
	m.notice = "previous error" // must be cleared on a successful action

	m.refresh(nil)

	if len(m.sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(m.sessions))
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (clamped)", m.cursor)
	}
	if m.notice != "" {
		t.Errorf("notice = %q, want empty (stale notice cleared on success)", m.notice)
	}
}

func TestDashboardRefresh_EmptyReloadClampsToZero(t *testing.T) {
	m := NewDashboard(
		[]state.Session{{ID: "a"}},
		func() ([]state.Session, error) { return nil, nil },
	)
	m.cursor = 0

	m.refresh(nil)

	if len(m.sessions) != 0 {
		t.Fatalf("sessions = %d, want 0", len(m.sessions))
	}
	if m.cursor != 0 {
		t.Errorf("cursor = %d, want 0 (not -1)", m.cursor)
	}
}

func TestDashboardRefresh_ActionErrorIsTransient(t *testing.T) {
	reloadCalled := false
	m := NewDashboard(
		[]state.Session{{ID: "a"}},
		func() ([]state.Session, error) { reloadCalled = true; return []state.Session{{ID: "a"}}, nil },
	)

	m.refresh(errors.New("kill failed"))

	if !reloadCalled {
		t.Errorf("reload should still run after an action error")
	}
	if m.notice != "kill failed" {
		t.Errorf("notice = %q, want the action error message", m.notice)
	}
	// The list survives (dashboard keeps running); the error is only a notice.
	if len(m.sessions) != 1 {
		t.Errorf("sessions = %d, want 1 (action error must not drop the list)", len(m.sessions))
	}
}

func TestDashboardRefresh_NilReloadIsSafe(t *testing.T) {
	m := NewDashboard([]state.Session{{ID: "a"}}, nil)

	m.refresh(nil) // must not panic with a nil reload

	if len(m.sessions) != 1 {
		t.Errorf("sessions unexpectedly changed with nil reload")
	}
}

func TestDashboardKillConfirmFlow(t *testing.T) {
	m := NewDashboard([]state.Session{{ID: "a", DisplayName: "app"}}, nil)

	// 'd' arms the confirmation without launching anything.
	m2, cmd := m.Update(runeKey('d'))
	dm := m2.(*DashboardModel)
	if dm.pendingKill != "a" {
		t.Fatalf("pendingKill = %q, want \"a\" after 'd'", dm.pendingKill)
	}
	if cmd != nil {
		t.Errorf("arming kill must not launch a command")
	}

	// A non-'y' key cancels without launching anything.
	_, cmd = dm.Update(runeKey('n'))
	if dm.pendingKill != "" {
		t.Errorf("pendingKill = %q, want cleared after cancel", dm.pendingKill)
	}
	if cmd != nil {
		t.Errorf("cancelling kill must not launch a command")
	}

	// Re-arm and confirm with 'y' → a command (the kill child) is returned.
	dm.Update(runeKey('d'))
	_, cmd = dm.Update(runeKey('y'))
	if dm.pendingKill != "" {
		t.Errorf("pendingKill should be cleared after confirm")
	}
	if cmd == nil {
		t.Errorf("confirming kill should launch the kill child command")
	}
}
