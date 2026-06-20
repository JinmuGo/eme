package cmd

import (
	"os"
	"testing"

	"github.com/jinmu/eme/internal/git"
	"github.com/jinmu/eme/internal/runner"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tui"
)

func TestAgentStatus(t *testing.T) {
	if got := agentStatus(&state.Worktree{}); got != tui.StatusIdle {
		t.Errorf("empty worktree = %v, want StatusIdle", got)
	}
	if got := agentStatus(&state.Worktree{AgentPID: os.Getpid()}); got != tui.StatusWorking {
		t.Errorf("live pid = %v, want StatusWorking", got)
	}
	if got := agentStatus(&state.Worktree{LastAgentCommand: "claude"}); got != tui.StatusExited {
		t.Errorf("ran-before = %v, want StatusExited", got)
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
			{Name: "main", Branch: "main"},
			{Name: "feat", Branch: "feat/x", AgentPID: os.Getpid(), LastAgentCommand: "claude"},
		},
	}}

	views := buildSessionViews(sessions)
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
