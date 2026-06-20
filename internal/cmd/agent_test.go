package cmd

import (
	"path/filepath"
	"testing"

	"github.com/jinmu/eme/internal/runner"
	"github.com/jinmu/eme/internal/state"
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
