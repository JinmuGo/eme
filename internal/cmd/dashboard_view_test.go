package cmd

import (
	"testing"

	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/runner"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
	"github.com/jinmu/eme/internal/tui"
)

func TestClassifyStatus(t *testing.T) {
	cases := []struct {
		name    string
		info    tmux.PaneInfo
		present bool
		last    string
		want    tui.AgentStatus
	}{
		{"never ran", tmux.PaneInfo{}, false, "", tui.StatusIdle},
		{"ran, window gone", tmux.PaneInfo{}, false, "claude", tui.StatusExited},
		{"alive agent", tmux.PaneInfo{Dead: false}, true, "claude", tui.StatusWorking},
		{"alive shell, no agent", tmux.PaneInfo{Dead: false}, true, "", tui.StatusIdle},
		{"clean exit", tmux.PaneInfo{Dead: true, DeadStatus: 0}, true, "claude", tui.StatusExited},
		{"crash", tmux.PaneInfo{Dead: true, DeadStatus: 3}, true, "claude", tui.StatusCrashed},
		// The agent runs under a different process name (claude => node); status must
		// not depend on matching Command to the agent name. pane_dead carries it.
		{"crash, node-named", tmux.PaneInfo{Dead: true, DeadStatus: 1, Command: "node"}, true, "claude", tui.StatusCrashed},
		{"running, node-named", tmux.PaneInfo{Dead: false, Command: "node"}, true, "claude", tui.StatusWorking},
	}
	for _, c := range cases {
		if got := classifyStatus(c.info, c.present, c.last); got != c.want {
			t.Errorf("%s: classifyStatus = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestAgentLabel(t *testing.T) {
	cases := map[string]string{
		"claude --dangerously": "claude",
		"/usr/bin/opencode":    "opencode",
		"":                     "",
	}
	for cmd, want := range cases {
		if got := agentLabel(&state.Worktree{LastAgentCommand: cmd}); got != want {
			t.Errorf("agentLabel(%q) = %q, want %q", cmd, got, want)
		}
	}
}

func TestBuildSessionViews_MapsFields(t *testing.T) {
	git.Runner = runner.NewMock()
	defer func() { git.Runner = runner.Default }()

	sessions := []state.Session{{
		ID:          "myapp-abc",
		DisplayName: "myapp",
		Root:        "/code/myapp",
		Worktrees: []state.Worktree{
			{Name: "main", Branch: "main", TmuxWindowID: "@1"},
			{Name: "feat", Branch: "feat/x", TmuxWindowID: "@2", LastAgentCommand: "claude"},
		},
	}}

	// @2 (feat) has a live pane running the agent (reported as node) → running.
	// @1 (main) recorded no agent → idle, regardless of its live shell pane.
	snap := map[string]tmux.PaneInfo{
		"@1": {Dead: false, Command: "zsh"},
		"@2": {Dead: false, Command: "node"},
	}

	views := buildSessionViews(sessions, snap)
	if len(views) != 1 || len(views[0].Worktrees) != 2 {
		t.Fatalf("unexpected shape: %+v", views)
	}
	main := views[0].Worktrees[0]
	if !main.IsMain || main.Status != tui.StatusIdle || main.SessionID != "myapp-abc" {
		t.Errorf("main view wrong: %+v", main)
	}
	feat := views[0].Worktrees[1]
	if feat.IsMain || feat.Status != tui.StatusWorking || feat.AgentLabel != "claude" {
		t.Errorf("feat view wrong: %+v", feat)
	}
}
