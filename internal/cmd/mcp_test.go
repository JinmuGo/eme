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

func seedOneProject(t *testing.T, dir string) {
	t.Helper()
	statePath = dir + "/state.json"
	seed := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "id1", DisplayName: "demo", Root: dir, TmuxName: "demo", Layout: state.LayoutNestedBare,
		Worktrees: []state.Worktree{{Name: "main", Branch: "main", Path: dir + "/main", TmuxWindowID: "@1"}},
	}}}
	if err := seed.Save(statePath); err != nil {
		t.Fatal(err)
	}
}

func TestMCPStartAgentIdempotentWhenRunning(t *testing.T) {
	dir := t.TempDir()
	oldState, oldRun, oldRunning := statePath, runEme, agentRunningFn
	defer func() { statePath, runEme, agentRunningFn = oldState, oldRun, oldRunning }()
	seedOneProject(t, dir)
	agentRunningFn = func(w *state.Worktree) (bool, error) { return true, nil }
	called := false
	runEme = func(args ...string) (string, string, error) { called = true; return "", "", nil }

	r, err := mcpStartAgent(context.Background(), "demo", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("runEme should not be called when an agent is already running")
	}
	if !r.Running || r.Message != "agent already running" {
		t.Fatalf("result = %+v", r)
	}
}

func TestMCPStartAgentLaunchesWhenIdle(t *testing.T) {
	dir := t.TempDir()
	oldState, oldRun, oldRunning := statePath, runEme, agentRunningFn
	defer func() { statePath, runEme, agentRunningFn = oldState, oldRun, oldRunning }()
	seedOneProject(t, dir)
	agentRunningFn = func(w *state.Worktree) (bool, error) { return false, nil }
	var gotArgs []string
	runEme = func(args ...string) (string, string, error) { gotArgs = args; return "", "", nil }

	r, err := mcpStartAgent(context.Background(), "demo", "main", "")
	if err != nil {
		t.Fatal(err)
	}
	if !r.Running || r.Message != "agent started" {
		t.Fatalf("result = %+v", r)
	}
	// expect a bare `agent id1 main` toggle (no --set, since no override)
	if len(gotArgs) != 3 || gotArgs[0] != "agent" || gotArgs[1] != "id1" || gotArgs[2] != "main" {
		t.Fatalf("args = %v", gotArgs)
	}
}

func TestMCPStopAgentNoopWhenIdle(t *testing.T) {
	dir := t.TempDir()
	oldState, oldRun, oldRunning := statePath, runEme, agentRunningFn
	defer func() { statePath, runEme, agentRunningFn = oldState, oldRun, oldRunning }()
	seedOneProject(t, dir)
	agentRunningFn = func(w *state.Worktree) (bool, error) { return false, nil }
	runEme = func(args ...string) (string, string, error) { t.Fatal("runEme should not run"); return "", "", nil }

	r, err := mcpStopAgent(context.Background(), "demo", "main")
	if err != nil {
		t.Fatal(err)
	}
	if r.Running || r.Message != "no agent running" {
		t.Fatalf("result = %+v", r)
	}
}

func TestNewMCPDepsIsFullyWired(t *testing.T) {
	d := newMCPDeps()
	if d.ServerVersion == "" {
		t.Error("ServerVersion empty")
	}
	if d.ListProjects == nil || d.GetProject == nil || d.ReadOutput == nil ||
		d.CreateProject == nil || d.CloneRepo == nil || d.CreateWorktree == nil ||
		d.StartAgent == nil || d.StopAgent == nil {
		t.Fatal("newMCPDeps left a nil function field")
	}
}

func TestMCPCommandRegistered(t *testing.T) {
	found := false
	for _, c := range rootCmd.Commands() {
		if c.Name() == "mcp" {
			found = true
		}
	}
	if !found {
		t.Fatal("mcp command not registered on rootCmd")
	}
}
