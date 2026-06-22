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
		// Live pane: status reads the FOREGROUND process. The agent runs under a
		// different name (claude => node), so a non-shell foreground means working;
		// a shell prompt means idle — even when an agent ran earlier (it has exited).
		{"running, node-named", tmux.PaneInfo{Dead: false, Command: "node"}, true, "claude", tui.StatusWorking},
		{"running, no record", tmux.PaneInfo{Dead: false, Command: "node"}, true, "", tui.StatusWorking},
		{"agent exited, back at prompt", tmux.PaneInfo{Dead: false, Command: "zsh"}, true, "claude", tui.StatusIdle},
		{"login shell prompt", tmux.PaneInfo{Dead: false, Command: "-zsh"}, true, "claude", tui.StatusIdle},
		{"alive shell, never ran", tmux.PaneInfo{Dead: false, Command: "bash"}, true, "", tui.StatusIdle},
		// An empty/unresolved foreground biases to idle, not a phantom running agent.
		{"empty foreground", tmux.PaneInfo{Dead: false, Command: ""}, true, "claude", tui.StatusIdle},
		// pane_dead is rare now (only a manually-killed/exited pane) but still maps.
		{"clean exit (dead pane)", tmux.PaneInfo{Dead: true, DeadStatus: 0}, true, "claude", tui.StatusExited},
		{"crash (dead pane)", tmux.PaneInfo{Dead: true, DeadStatus: 3}, true, "claude", tui.StatusCrashed},
		// A hook-pushed @eme_state refines the live non-shell case into a precise state.
		{"hook: waiting", tmux.PaneInfo{Dead: false, Command: "node", EmeState: "waiting"}, true, "claude", tui.StatusWaiting},
		{"hook: working", tmux.PaneInfo{Dead: false, Command: "node", EmeState: "working"}, true, "claude", tui.StatusWorking},
		{"hook: done while agent alive", tmux.PaneInfo{Dead: false, Command: "node", EmeState: "done"}, true, "claude", tui.StatusIdle},
		{"hook: crashed while agent alive", tmux.PaneInfo{Dead: false, Command: "node", EmeState: "crashed"}, true, "claude", tui.StatusCrashed},
		{"unknown hook value falls back to working", tmux.PaneInfo{Dead: false, Command: "node", EmeState: "banana"}, true, "claude", tui.StatusWorking},
		// Ground-truth precedence: a shell prompt is idle even if a stale @eme_state lingers
		// (the agent crashed/quit and returned to the shell — the option was never cleared).
		{"shell prompt beats stale hook state", tmux.PaneInfo{Dead: false, Command: "zsh", EmeState: "working"}, true, "claude", tui.StatusIdle},
	}
	for _, c := range cases {
		if got := classifyStatus(c.info, c.present, c.last); got != c.want {
			t.Errorf("%s: classifyStatus = %v, want %v", c.name, got, c.want)
		}
	}
}

// TestIsShellCommand_HonorsUserShellAndModernShells: a pane is idle when its
// foreground is any common shell, the user's own $SHELL (even if exotic), or empty
// (unresolved). Anything else (an agent / a running command) is not a shell.
func TestIsShellCommand_HonorsUserShellAndModernShells(t *testing.T) {
	t.Setenv("SHELL", "/usr/bin/exoticsh") // not in the static set
	for _, c := range []struct {
		cmd  string
		want bool
	}{
		{"zsh", true}, {"-zsh", true}, {"/bin/bash", true}, // common + login + path
		{"fish", true}, {"nu", true}, {"pwsh", true}, // modern shells
		{"exoticsh", true},             // the user's own $SHELL basename
		{"", true},                     // empty/unresolved foreground biases to idle
		{"node", false},                // claude runs as node — a working agent
		{"vim", false}, {"git", false}, // running commands are not idle
	} {
		if got := isShellCommand(c.cmd); got != c.want {
			t.Errorf("isShellCommand(%q) = %v, want %v", c.cmd, got, c.want)
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

// TestBuildStatusViews_SkipsGitDiff locks T3: the status-only path classifies agent
// status but never shells out to git (no DiffStat), so the ticker stays cheap.
func TestBuildStatusViews_SkipsGitDiff(t *testing.T) {
	mock := runner.NewMock()
	git.Runner = mock
	defer func() { git.Runner = runner.Default }()

	sessions := []state.Session{{
		ID: "myapp", DisplayName: "myapp", Root: "/code/myapp",
		Worktrees: []state.Worktree{
			{Name: "feat", Branch: "feat/x", TmuxWindowID: "@2", LastAgentCommand: "claude"},
		},
	}}
	snap := map[string]tmux.PaneInfo{"@2": {Dead: false, Command: "node"}}

	views := buildStatusViews(sessions, snap)
	if len(mock.Calls) != 0 {
		t.Errorf("status-only build must not shell out to git, made %d call(s): %+v", len(mock.Calls), mock.Calls)
	}
	w := views[0].Worktrees[0]
	if w.Status != tui.StatusWorking {
		t.Errorf("status = %v, want StatusWorking", w.Status)
	}
	if w.HasDiff {
		t.Error("status-only build must not populate diff")
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
