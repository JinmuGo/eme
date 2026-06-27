package cmd

import (
	"testing"

	"github.com/alderwork/eme/internal/state"
	"github.com/alderwork/eme/internal/tmux"
	"github.com/alderwork/eme/internal/tui"
)

func TestAgentStatusString(t *testing.T) {
	cases := map[tui.AgentStatus]string{
		tui.StatusIdle:    "idle",
		tui.StatusWorking: "working",
		tui.StatusWaiting: "waiting-for-input",
		tui.StatusCrashed: "crashed",
		tui.StatusExited:  "exited",
	}
	for in, want := range cases {
		if got := agentStatusString(in); got != want {
			t.Errorf("agentStatusString(%v) = %q, want %q", in, got, want)
		}
	}
}

func TestToMCPProjectMapsWorktrees(t *testing.T) {
	sess := &state.Session{
		ID:          "id1",
		DisplayName: "demo",
		Root:        "/tmp/demo",
		Layout:      state.LayoutNestedBare,
		Worktrees: []state.Worktree{
			{Name: "main", Branch: "main", Path: "/tmp/demo/main", TmuxWindowID: "@1"},
			{Name: "feat", Branch: "feat", Path: "/tmp/demo/feat", TmuxWindowID: "@2", LastAgentCommand: "claude"},
		},
	}
	// @1 idle (shell), @2 working (non-shell foreground)
	snap := map[string]tmux.PaneInfo{
		"@1": {Command: "zsh"},
		"@2": {Command: "node"},
	}
	p := toMCPProject(sess, snap)
	if p.ID != "id1" || p.DisplayName != "demo" || p.Layout != state.LayoutNestedBare {
		t.Fatalf("project header = %+v", p)
	}
	if len(p.Worktrees) != 2 {
		t.Fatalf("want 2 worktrees, got %d", len(p.Worktrees))
	}
	if p.Worktrees[0].AgentStatus != "idle" {
		t.Errorf("main status = %q, want idle", p.Worktrees[0].AgentStatus)
	}
	if p.Worktrees[1].AgentStatus != "working" || p.Worktrees[1].AgentCommand != "claude" {
		t.Errorf("feat = %+v", p.Worktrees[1])
	}
}
