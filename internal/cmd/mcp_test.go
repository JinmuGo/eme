package cmd

import (
	"context"
	"errors"
	"testing"

	"github.com/alderwork/eme/internal/session"
	"github.com/alderwork/eme/internal/state"
	"github.com/alderwork/eme/internal/tmux"
	"github.com/alderwork/eme/internal/tui"
)

var errFakeExit = errors.New("exit status 1")

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

func TestMCPCreateProjectReadsBackState(t *testing.T) {
	dir := t.TempDir()
	oldState, oldSocket, oldRun := statePath, tmux.Socket, runEme
	defer func() { statePath, tmux.Socket, runEme = oldState, oldSocket, oldRun }()
	statePath = dir + "/state.json"
	tmux.Socket = "eme-test-nonexistent" // force PanesSnapshot to fail → deterministic idle

	folder := dir + "/proj"
	runEme = func(args ...string) (string, string, error) {
		s, _ := state.Load(statePath)
		s.AddSession(state.Session{
			ID: session.ID(folder), DisplayName: "proj", Root: folder, TmuxName: "proj",
			Layout: state.LayoutNestedBare,
			Worktrees: []state.Worktree{{Name: "main", Branch: "main", Path: folder + "/main", TmuxWindowID: "@9"}},
		})
		_ = s.Save(statePath)
		return "", "", nil
	}

	p, err := mcpCreateProject(context.Background(), folder, "none")
	if err != nil {
		t.Fatalf("mcpCreateProject: %v", err)
	}
	if p.DisplayName != "proj" || len(p.Worktrees) != 1 || p.Worktrees[0].Name != "main" {
		t.Fatalf("project = %+v", p)
	}
	if p.Worktrees[0].AgentStatus != "idle" {
		t.Fatalf("status = %q, want idle", p.Worktrees[0].AgentStatus)
	}
}

func TestMCPCreateProjectSurfacesEmeError(t *testing.T) {
	dir := t.TempDir()
	oldState, oldRun := statePath, runEme
	defer func() { statePath, runEme = oldState, oldRun }()
	statePath = dir + "/state.json"
	runEme = func(args ...string) (string, string, error) {
		return "", "eme: That folder is a bare git repository.", errFakeExit
	}
	_, err := mcpCreateProject(context.Background(), dir+"/x", "none")
	if err == nil || err.Error() != "That folder is a bare git repository." {
		t.Fatalf("err = %v", err)
	}
}

func TestMCPCreateWorktreeReadsBack(t *testing.T) {
	dir := t.TempDir()
	oldState, oldSocket, oldRun := statePath, tmux.Socket, runEme
	defer func() { statePath, tmux.Socket, runEme = oldState, oldSocket, oldRun }()
	statePath = dir + "/state.json"
	tmux.Socket = "eme-test-nonexistent"

	// seed an existing project
	seed := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "id1", DisplayName: "demo", Root: dir, TmuxName: "demo", Layout: state.LayoutNestedBare,
		Worktrees: []state.Worktree{{Name: "main", Branch: "main", Path: dir + "/main", TmuxWindowID: "@1"}},
	}}}
	if err := seed.Save(statePath); err != nil {
		t.Fatal(err)
	}
	runEme = func(args ...string) (string, string, error) {
		s, _ := state.Load(statePath)
		s.Sessions[0].AddWorktree(state.Worktree{Name: "feat", Branch: "feat", Path: dir + "/feat", TmuxWindowID: "@2"})
		_ = s.Save(statePath)
		return "", "", nil
	}
	w, err := mcpCreateWorktree(context.Background(), "demo", "feat", "none")
	if err != nil {
		t.Fatalf("mcpCreateWorktree: %v", err)
	}
	if w.Name != "feat" || w.Branch != "feat" {
		t.Fatalf("worktree = %+v", w)
	}
}
