package cmd

import (
	"reflect"
	"syscall"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alderwork/eme/internal/tui"
)

// captureExec swaps execReplace for one that records its argv instead of
// replacing the process, and restores it when the test ends.
func captureExec(t *testing.T, got *[]string, called *bool) {
	t.Helper()
	execReplace = func(_ string, argv []string, _ []string) error {
		*called = true
		*got = argv
		return nil
	}
	t.Cleanup(func() { execReplace = syscall.Exec })
}

func dashboardWith(name, worktree string, isMain bool) *tui.DashboardModel {
	m := tui.NewDashboard([]tui.SessionView{
		{DisplayName: name, Worktrees: []tui.WorktreeView{
			{Name: worktree, SessionID: name, IsMain: isMain},
		}},
	}, nil)
	// Row 0 is the session header; step onto the worktree row these tests target.
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	return m
}

// Enter on a worktree must hand the cmd layer the right `eme switch` argv.
func TestSwitchFromModel_ExecsRecordedTarget(t *testing.T) {
	var argv []string
	var called bool
	captureExec(t, &argv, &called)

	m := dashboardWith("myapp", "feat", false)
	m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // records the switch target + quits

	if err := switchFromModel(m); err != nil {
		t.Fatalf("switchFromModel: %v", err)
	}
	want := []string{"eme", "switch", "myapp", "feat"}
	if !called || !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %v (called=%v), want %v", argv, called, want)
	}
}

// Quitting without selecting (no recorded target) must NOT exec anything.
func TestSwitchFromModel_NoTargetIsNoop(t *testing.T) {
	var argv []string
	var called bool
	captureExec(t, &argv, &called)

	m := dashboardWith("myapp", "main", true)
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}) // quit, no switch

	if err := switchFromModel(m); err != nil {
		t.Fatalf("switchFromModel: %v", err)
	}
	if called {
		t.Errorf("no switch target must not exec; got argv %v", argv)
	}
}

// A non-dashboard final model is a safe no-op.
func TestSwitchFromModel_WrongModelIsNoop(t *testing.T) {
	var called bool
	var argv []string
	captureExec(t, &argv, &called)

	if err := switchFromModel(tui.NewInput("x")); err != nil {
		t.Fatalf("switchFromModel: %v", err)
	}
	if called {
		t.Error("non-dashboard model must not exec")
	}
}

// The defensive empty-worktree branch drops the trailing arg.
func TestExecSwitch_OmitsEmptyWorktree(t *testing.T) {
	var argv []string
	var called bool
	captureExec(t, &argv, &called)

	if err := execSwitch("myapp", ""); err != nil {
		t.Fatalf("execSwitch: %v", err)
	}
	want := []string{"eme", "switch", "myapp"}
	if !reflect.DeepEqual(argv, want) {
		t.Errorf("argv = %v, want %v", argv, want)
	}
}
