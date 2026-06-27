package cmd

import (
	"testing"

	"github.com/alderwork/eme/internal/runner"
	"github.com/alderwork/eme/internal/state"
	"github.com/alderwork/eme/internal/tmux"
)

// hermeticTmux swaps tmux.Runner for an unstubbed mock so RespawnPane never reaches
// a real tmux server during a test (its result is ignored by cleanWorktree anyway).
func hermeticTmux(t *testing.T) {
	t.Helper()
	prev := tmux.Runner
	tmux.Runner = runner.NewMock()
	t.Cleanup(func() { tmux.Runner = prev })
}

// TestCleanWorktree_ClearsAgentRecord: cleaning a finished worktree clears the
// recorded agent so the (now-respawned) live shell reads idle, not a false running.
func TestCleanWorktree_ClearsAgentRecord(t *testing.T) {
	stubAgentRunning(t, false, nil)
	hermeticTmux(t)

	sess := &state.Session{ID: "myapp", TmuxName: "myapp"}
	w := &state.Worktree{
		Name: "feat", TmuxWindowID: "@2", Path: "/code/myapp/feat",
		LastAgentCommand: "claude", AgentPID: 1234,
	}

	if err := cleanWorktree(sess, w); err != nil {
		t.Fatalf("cleanWorktree returned error: %v", err)
	}
	if w.LastAgentCommand != "" {
		t.Errorf("LastAgentCommand = %q, want cleared", w.LastAgentCommand)
	}
	if w.AgentPID != 0 {
		t.Errorf("AgentPID = %d, want 0", w.AgentPID)
	}
}

// TestCleanWorktree_RefusesWhenAgentRunning: a live agent must not be cleaned —
// clearing the record would misreport the running agent as idle.
func TestCleanWorktree_RefusesWhenAgentRunning(t *testing.T) {
	stubAgentRunning(t, true, nil)
	hermeticTmux(t)

	sess := &state.Session{ID: "myapp", TmuxName: "myapp"}
	w := &state.Worktree{Name: "feat", TmuxWindowID: "@2", LastAgentCommand: "claude"}

	if err := cleanWorktree(sess, w); err == nil {
		t.Fatal("cleanWorktree should refuse while the agent is running")
	}
	if w.LastAgentCommand == "" {
		t.Error("must not clear the record when refusing")
	}
}
