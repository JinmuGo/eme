package tui

import "testing"

func animWorkingViews() []SessionView {
	return []SessionView{{DisplayName: "app", Root: "/app", Worktrees: []WorktreeView{
		{Name: "w", SessionID: "app", Status: StatusWorking},
	}}}
}

func animIdleViews() []SessionView {
	return []SessionView{{DisplayName: "app", Root: "/app", Worktrees: []WorktreeView{
		{Name: "a", SessionID: "app", Status: StatusIdle},
	}}}
}

// hasAnimatingAgent is true only for a live, non-quiet working agent.
func TestHasAnimatingAgent(t *testing.T) {
	if !NewDashboard(animWorkingViews(), nil).hasAnimatingAgent() {
		t.Error("a live working agent should animate")
	}
	if NewDashboard(animIdleViews(), nil).hasAnimatingAgent() {
		t.Error("an idle-only dashboard must not animate")
	}
	quiet := []SessionView{{DisplayName: "app", Root: "/app", Worktrees: []WorktreeView{
		{Name: "q", SessionID: "app", Status: StatusWorking, Quiet: true},
	}}}
	if NewDashboard(quiet, nil).hasAnimatingAgent() {
		t.Error("a gone-quiet working agent must not animate")
	}
}

// Init arms the spinner ticker exactly when a working agent is present.
func TestInitArmsAnimationWhenWorking(t *testing.T) {
	m := NewDashboard(animWorkingViews(), nil)
	m.Init()
	if !m.animating {
		t.Error("Init should arm the spinner ticker when a working agent is on screen")
	}
	idle := NewDashboard(animIdleViews(), nil)
	idle.Init()
	if idle.animating {
		t.Error("Init must not arm the spinner ticker on an idle dashboard")
	}
}

// The 2s data tick no longer advances the spinner frame — motion owns its own clock.
func TestDataTickDoesNotAdvanceSpinner(t *testing.T) {
	m := NewDashboard(animWorkingViews(), nil)
	before := m.glyphFrame
	m.Update(tickMsg{})
	if m.glyphFrame != before {
		t.Errorf("tickMsg must not advance glyphFrame (got %d, want %d)", m.glyphFrame, before)
	}
}

// An animation tick advances the spinner and reschedules while a worker remains; it stops
// itself (nil cmd, animating cleared) when nothing is left to spin.
func TestAnimTickAdvancesAndStops(t *testing.T) {
	m := NewDashboard(animWorkingViews(), nil)
	m.animating = true
	_, cmd := m.Update(animTickMsg{})
	if m.glyphFrame != 1 {
		t.Errorf("animTickMsg should advance glyphFrame to 1, got %d", m.glyphFrame)
	}
	if cmd == nil {
		t.Error("animTickMsg should reschedule while a working agent remains")
	}

	idle := NewDashboard(animIdleViews(), nil)
	idle.animating = true
	_, cmd = idle.Update(animTickMsg{})
	if cmd != nil {
		t.Error("animTickMsg should not reschedule when nothing is animating")
	}
	if idle.animating {
		t.Error("animTickMsg should clear the animating flag when nothing spins")
	}
}
