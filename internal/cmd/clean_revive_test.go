package cmd

import (
	"testing"

	"github.com/jinmu/eme/internal/runner"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tmux"
)

// paneSnapFormat mirrors the -F format tmux.PanesSnapshot uses; the mock key must
// match it byte-for-byte (note the literal tabs).
const paneSnapFormat = "#{window_id}\t#{pane_dead}\t#{pane_dead_status}\t#{pane_current_command}\t#{@eme_state}\t#{@eme_state_at}"

func calledRespawn(m *runner.Mock, target string) bool {
	for _, c := range m.Calls {
		if c.Name == "tmux" && len(c.Args) >= 3 && c.Args[0] == "respawn-pane" && c.Args[2] == target {
			return true
		}
	}
	return false
}

func calledRemainOff(m *runner.Mock, target string) bool {
	for _, c := range m.Calls {
		if c.Name == "tmux" && len(c.Args) == 6 && c.Args[0] == "set-option" &&
			c.Args[3] == target && c.Args[4] == "remain-on-exit" && c.Args[5] == "off" {
			return true
		}
	}
	return false
}

// TestReviveIfDead_RevivesDeadPaneAndClearsRecord: a dead pane (agent exited under
// remain-on-exit) is respawned to a fresh shell and the recorded agent cleared, so a
// switch lands the user on a usable pane reading idle.
func TestReviveIfDead_RevivesDeadPaneAndClearsRecord(t *testing.T) {
	tempState(t)
	mock := runner.NewMock()
	mock.Set("tmux", []string{"list-panes", "-a", "-F", paneSnapFormat}, "@10\t1\t0\tzsh", "", nil)
	mock.Set("tmux", []string{"respawn-pane", "-t", "proj:@10", "-c", "/x/main"}, "", "", nil)
	mock.Set("tmux", []string{"set-option", "-w", "-t", "proj:@10", "remain-on-exit", "off"}, "", "", nil)
	prev := tmux.Runner
	tmux.Runner = mock
	t.Cleanup(func() { tmux.Runner = prev })

	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "proj-x", TmuxName: "proj", DisplayName: "proj",
		Worktrees: []state.Worktree{{
			Name: "main", Path: "/x/main", TmuxWindowID: "@10",
			LastAgentCommand: "claude", AgentPID: 4321,
		}},
	}}}
	sess := &s.Sessions[0]
	w := &sess.Worktrees[0]

	reviveIfDead(s, sess, w)

	if !calledRespawn(mock, "proj:@10") {
		t.Error("expected respawn-pane on the dead pane")
	}
	if !calledRemainOff(mock, "proj:@10") {
		t.Error("expected remain-on-exit set off after reviving to a bare shell")
	}
	if w.LastAgentCommand != "" || w.AgentPID != 0 {
		t.Errorf("recorded agent not cleared: %+v", *w)
	}
}

// TestReviveIfDead_DeadPaneEmptyRecordSkipsClear: a dead pane whose record is already
// empty is still revived (respawn + remain-off), but the clear/saveState is skipped
// (the guard), so an already-idle record stays as-is without a redundant write.
func TestReviveIfDead_DeadPaneEmptyRecordSkipsClear(t *testing.T) {
	tempState(t) // guards against an unexpected write escaping to real state
	mock := runner.NewMock()
	mock.Set("tmux", []string{"list-panes", "-a", "-F", paneSnapFormat}, "@10\t1\t0\tzsh", "", nil)
	mock.Set("tmux", []string{"respawn-pane", "-t", "proj:@10", "-c", "/x/main"}, "", "", nil)
	mock.Set("tmux", []string{"set-option", "-w", "-t", "proj:@10", "remain-on-exit", "off"}, "", "", nil)
	prev := tmux.Runner
	tmux.Runner = mock
	t.Cleanup(func() { tmux.Runner = prev })

	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "proj-x", TmuxName: "proj", DisplayName: "proj",
		Worktrees: []state.Worktree{{
			Name: "main", Path: "/x/main", TmuxWindowID: "@10",
			LastAgentCommand: "", AgentPID: 0, // already idle
		}},
	}}}
	sess := &s.Sessions[0]
	w := &sess.Worktrees[0]

	reviveIfDead(s, sess, w)

	if !calledRespawn(mock, "proj:@10") {
		t.Error("a dead pane must still be revived even when the record is already empty")
	}
	if w.LastAgentCommand != "" || w.AgentPID != 0 {
		t.Errorf("record must remain empty: %+v", *w)
	}
}

// TestReviveIfDead_LeavesLivePaneUntouched: a live pane (agent running or idle shell)
// is never respawned and its record is preserved.
func TestReviveIfDead_LeavesLivePaneUntouched(t *testing.T) {
	mock := runner.NewMock()
	mock.Set("tmux", []string{"list-panes", "-a", "-F", paneSnapFormat}, "@10\t0\t0\tnode", "", nil)
	prev := tmux.Runner
	tmux.Runner = mock
	t.Cleanup(func() { tmux.Runner = prev })

	s := &state.State{Version: state.Version, Sessions: []state.Session{{
		ID: "proj-x", TmuxName: "proj", DisplayName: "proj",
		Worktrees: []state.Worktree{{
			Name: "main", Path: "/x/main", TmuxWindowID: "@10",
			LastAgentCommand: "claude", AgentPID: 4321,
		}},
	}}}
	sess := &s.Sessions[0]
	w := &sess.Worktrees[0]

	reviveIfDead(s, sess, w)

	if calledRespawn(mock, "proj:@10") {
		t.Error("must not respawn a live pane")
	}
	if w.LastAgentCommand != "claude" || w.AgentPID != 4321 {
		t.Errorf("must not clear the record for a live pane: %+v", *w)
	}
}
