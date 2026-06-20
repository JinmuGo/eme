package cmd

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/jinmu/eme/internal/config"
	"github.com/jinmu/eme/internal/runner"
	"github.com/jinmu/eme/internal/state"
	"github.com/jinmu/eme/internal/tui"
)

// stubWhich makes runner.Default answer `which <bin>` successfully and restores
// it on cleanup.
func stubWhich(t *testing.T, bin string) {
	t.Helper()
	prev := runner.Default
	mock := runner.NewMock()
	mock.Set("which", []string{bin}, "/usr/local/bin/"+bin, "", nil)
	runner.Default = mock
	t.Cleanup(func() { runner.Default = prev })
}

// captureSendKeys records the last (target, line) sent and restores sendKeys.
func captureSendKeys(t *testing.T, target, line *string) {
	t.Helper()
	prev := sendKeys
	sendKeys = func(tgt, keys string) error {
		*target = tgt
		*line = keys
		return nil
	}
	t.Cleanup(func() { sendKeys = prev })
}

// tempState points statePath at a throwaway state file for the duration of the
// test and restores it afterward, so saveState writes never escape the test and
// later tests in the same package never inherit a stale path.
func tempState(t *testing.T) {
	t.Helper()
	prev := statePath
	statePath = filepath.Join(t.TempDir(), "state.json")
	t.Cleanup(func() { statePath = prev })
}

// tempCfg installs a default config in the package global for the duration of
// the test and restores it on cleanup, so helpers that read cfg (e.g.
// chooseAndLaunchAgent → cfg.Catalog()) see production-shaped config instead of
// a nil global.
func tempCfg(t *testing.T) {
	t.Helper()
	prev := cfg
	cfg = config.Default()
	t.Cleanup(func() { cfg = prev })
}

func TestAgentItems_MarksInstalledAndAppendsNone(t *testing.T) {
	prev := lookPath
	lookPath = func(bin string) (string, error) {
		if bin == "claude" {
			return "/usr/local/bin/claude", nil
		}
		return "", fmt.Errorf("not found")
	}
	t.Cleanup(func() { lookPath = prev })

	items := agentItems([]config.AgentSpec{
		{Name: "claude", Command: "claude"},
		{Name: "codex", Command: "codex"},
	})

	if len(items) != 3 { // 2 agents + none
		t.Fatalf("len(items) = %d, want 3", len(items))
	}
	if !items[0].Installed || items[0].Name != "claude" {
		t.Errorf("items[0] = %+v, want installed claude", items[0])
	}
	if items[1].Installed {
		t.Errorf("codex should be not-installed: %+v", items[1])
	}
	if !items[2].None || !items[2].Installed {
		t.Errorf("last item should be installed none row: %+v", items[2])
	}
}

func TestChooseAndLaunchAgent_AppliesAndLaunchesOnSelection(t *testing.T) {
	tempCfg(t)
	tempState(t)
	stubWhich(t, "claude")
	var line string
	var target string
	captureSendKeys(t, &target, &line)

	prevPick := pickAgent
	pickAgent = func(items []tui.AgentItem, def string) (tui.AgentItem, bool, bool, error) {
		return tui.AgentItem{Name: "claude", Command: "claude", Installed: true}, false, false, nil
	}
	t.Cleanup(func() { pickAgent = prevPick })
	prevLook := lookPath
	lookPath = func(bin string) (string, error) { return "/x/" + bin, nil }
	t.Cleanup(func() { lookPath = prevLook })

	s := &state.State{Version: state.Version}
	sess := &state.Session{TmuxName: "myapp", DisplayName: "myapp"}
	w := &state.Worktree{Name: "main", Path: "/p/main", TmuxWindowID: "@1"}

	var applied string
	err := chooseAndLaunchAgent(s, sess, w, "", func(cmd string) { applied = cmd })
	if err != nil {
		t.Fatalf("chooseAndLaunchAgent: %v", err)
	}
	if applied != "claude" {
		t.Errorf("apply got %q, want claude", applied)
	}
	if line != "claude" {
		t.Errorf("launched line = %q, want claude", line)
	}
}

func TestChooseAndLaunchAgent_NoneDoesNotApplyOrLaunch(t *testing.T) {
	tempCfg(t)
	tempState(t)
	var line string
	var target string
	captureSendKeys(t, &target, &line)

	prevPick := pickAgent
	pickAgent = func(items []tui.AgentItem, def string) (tui.AgentItem, bool, bool, error) {
		return tui.AgentItem{}, true, false, nil // none
	}
	t.Cleanup(func() { pickAgent = prevPick })
	prevLook := lookPath
	lookPath = func(bin string) (string, error) { return "/x/" + bin, nil }
	t.Cleanup(func() { lookPath = prevLook })

	s := &state.State{Version: state.Version}
	sess := &state.Session{TmuxName: "myapp"}
	w := &state.Worktree{Name: "main", TmuxWindowID: "@1"}

	applied := false
	if err := chooseAndLaunchAgent(s, sess, w, "", func(string) { applied = true }); err != nil {
		t.Fatalf("err: %v", err)
	}
	if applied {
		t.Error("apply must not be called for none")
	}
	if line != "" {
		t.Errorf("must not launch for none; sent %q", line)
	}
}

func TestChooseAndLaunchAgent_SkipsPickerWhenNothingInstalled(t *testing.T) {
	tempCfg(t)
	prevLook := lookPath
	lookPath = func(bin string) (string, error) { return "", fmt.Errorf("not found") }
	t.Cleanup(func() { lookPath = prevLook })
	prevPick := pickAgent
	pickAgent = func(items []tui.AgentItem, def string) (tui.AgentItem, bool, bool, error) {
		t.Fatal("picker must not run when no agents are installed")
		return tui.AgentItem{}, false, true, nil
	}
	t.Cleanup(func() { pickAgent = prevPick })

	s := &state.State{Version: state.Version}
	sess := &state.Session{TmuxName: "x"}
	w := &state.Worktree{Name: "main", TmuxWindowID: "@1"}

	if err := chooseAndLaunchAgent(s, sess, w, "", func(string) {}); err != nil {
		t.Fatalf("err: %v", err)
	}
}

func TestAgentPickFlagRegistered(t *testing.T) {
	if agentCmd.Flags().Lookup("pick") == nil {
		t.Errorf("--pick flag not registered on agentCmd")
	}
}

func TestLaunchAgentCommand_SendsBareCommand(t *testing.T) {
	tempState(t)
	stubWhich(t, "claude")
	var gotTarget, gotLine string
	captureSendKeys(t, &gotTarget, &gotLine)

	s := &state.State{Version: state.Version}
	sess := &state.Session{TmuxName: "myapp", DisplayName: "myapp"}
	w := &state.Worktree{Name: "main", Path: "/p/myapp/main", TmuxWindowID: "@1"}

	if err := launchAgentCommand(s, sess, w, "claude"); err != nil {
		t.Fatalf("launchAgentCommand: %v", err)
	}
	if gotTarget != "myapp:@1" {
		t.Errorf("target = %q, want %q", gotTarget, "myapp:@1")
	}
	// Regression: the pane cwd is already the worktree, so NO path argument.
	if gotLine != "claude" {
		t.Errorf("sent line = %q, want bare %q (no path arg)", gotLine, "claude")
	}
}
