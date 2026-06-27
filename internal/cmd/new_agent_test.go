package cmd

import (
	"fmt"
	"testing"

	"github.com/alderwork/eme/internal/state"
	"github.com/alderwork/eme/internal/tui"
)

// TestMaybeOnboardWorktreeAgent_SetsOverrideAndLaunches: a freshly created worktree gets
// the same agent onboarding as a new project — the picker's choice is recorded as that
// worktree's OWN agent (an override, not the session default) and launched in the
// worktree's window, never main's. This is the worktree-per-agent flow the dashboard's
// `c` (create worktree) relies on; without it `c` left a bare shell with no agent picker.
func TestMaybeOnboardWorktreeAgent_SetsOverrideAndLaunches(t *testing.T) {
	tempState(t)
	tempCfg(t)
	stubWhich(t, "opencode")
	stubAgentRunning(t, false, nil) // hermetic: keep the launch pre-guard off real tmux
	var target, line string
	captureSendKeys(t, &target, &line)

	prevLook := lookPath
	lookPath = func(bin string) (string, error) { return "/x/" + bin, nil }
	t.Cleanup(func() { lookPath = prevLook })
	prevPick := pickAgent
	pickAgent = func(items []tui.AgentItem, def string) (tui.AgentItem, bool, bool, error) {
		return tui.AgentItem{Name: "opencode", Command: "opencode", Installed: true}, false, false, nil
	}
	t.Cleanup(func() { pickAgent = prevPick })

	s := &state.State{Version: state.Version}
	sess := &state.Session{
		TmuxName:    "myapp",
		DisplayName: "myapp",
		Worktrees: []state.Worktree{
			{Name: "main", Path: "/p/main", TmuxWindowID: "@1"},
			{Name: "feat", Path: "/p/feat", TmuxWindowID: "@2"},
		},
	}

	maybeOnboardWorktreeAgent(s, sess, "feat")

	w := sess.WorktreeByName("feat")
	if w.AgentCommandOverride != "opencode" {
		t.Errorf("feat AgentCommandOverride = %q, want opencode", w.AgentCommandOverride)
	}
	if sess.AgentCommand != "" {
		t.Errorf("worktree onboarding must not set the session default, got AgentCommand=%q", sess.AgentCommand)
	}
	if want := "myapp:@2"; target != want {
		t.Errorf("agent launched in window %q, want feat's window %q", target, want)
	}
}

func TestMaybeOnboardAgent_SetsProjectDefaultAndLaunches(t *testing.T) {
	tempState(t)
	tempCfg(t)
	stubWhich(t, "opencode")
	var target, line string
	captureSendKeys(t, &target, &line)

	prevLook := lookPath
	lookPath = func(bin string) (string, error) { return "/x/" + bin, nil }
	t.Cleanup(func() { lookPath = prevLook })
	prevPick := pickAgent
	pickAgent = func(items []tui.AgentItem, def string) (tui.AgentItem, bool, bool, error) {
		return tui.AgentItem{Name: "opencode", Command: "opencode", Installed: true}, false, false, nil
	}
	t.Cleanup(func() { pickAgent = prevPick })

	s := &state.State{Version: state.Version}
	sess := &state.Session{
		TmuxName:    "myapp",
		DisplayName: "myapp",
		Worktrees:   []state.Worktree{{Name: "main", Path: "/p/main", TmuxWindowID: "@1"}},
	}

	maybeOnboardAgent(s, sess)

	if sess.AgentCommand != "opencode" {
		t.Errorf("sess.AgentCommand = %q, want opencode", sess.AgentCommand)
	}
}

func TestMaybeOnboardAgent_NeverBlocksWhenNothingInstalled(t *testing.T) {
	tempState(t)
	tempCfg(t)
	prevLook := lookPath
	lookPath = func(bin string) (string, error) { return "", fmt.Errorf("nope") }
	t.Cleanup(func() { lookPath = prevLook })
	prevPick := pickAgent
	pickAgent = func(items []tui.AgentItem, def string) (tui.AgentItem, bool, bool, error) {
		t.Fatal("onboarding must not open the picker with no agents installed")
		return tui.AgentItem{}, false, true, nil
	}
	t.Cleanup(func() { pickAgent = prevPick })

	s := &state.State{Version: state.Version}
	sess := &state.Session{
		TmuxName:  "x",
		Worktrees: []state.Worktree{{Name: "main", TmuxWindowID: "@1"}},
	}
	maybeOnboardAgent(s, sess) // must return without calling the picker
}
